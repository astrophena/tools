// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// vim: foldmethod=marker

package main

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/request"
	"go.astrophena.name/base/syncx"
	"go.astrophena.name/base/tgauth"
	"go.astrophena.name/base/txtar"
	"go.astrophena.name/base/version"
	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/cmd/starlet/internal/bot"
	"go.astrophena.name/tools/internal/api/github/gist"
	"go.astrophena.name/tools/internal/api/google/gemini"
	"go.astrophena.name/tools/internal/starlark/lib/kvcache"
	"go.astrophena.name/tools/internal/util/logstream"

	"github.com/lmittmann/tint"
)

const tgAPI = "https://api.telegram.org"

func main() { cli.Main(new(engine)) }

func (e *engine) Run(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	if len(env.Args) > 1 {
		return errors.New("too many arguments")
	}

	// Load configuration from environment variables.
	e.addr = cmp.Or(e.addr, env.Getenv("ADDR"), "localhost:3000")
	e.geminiKey = cmp.Or(e.geminiKey, env.Getenv("GEMINI_KEY"))
	e.geminiProxyToken = cmp.Or(e.geminiProxyToken, env.Getenv("GEMINI_PROXY_TOKEN"))
	e.ghToken = cmp.Or(e.ghToken, env.Getenv("GH_TOKEN"))
	e.gistID = cmp.Or(e.gistID, env.Getenv("GIST_ID"))
	e.host = cmp.Or(e.host, env.Getenv("HOST"))
	e.onRender = env.Getenv("RENDER") == "true"
	e.pingURL = cmp.Or(e.pingURL, env.Getenv("PING_URL"))
	e.reloadToken = cmp.Or(e.reloadToken, env.Getenv("RELOAD_TOKEN"))
	e.tgOwner = cmp.Or(e.tgOwner, parseInt(env.Getenv("TG_OWNER")))
	e.tgSecret = cmp.Or(e.tgSecret, env.Getenv("TG_SECRET"))
	e.tgToken = cmp.Or(e.tgToken, env.Getenv("TG_TOKEN"))
	if e.onRender {
		e.dev = false
	}
	e.stderr = env.Stderr
	if len(env.Args) == 1 {
		e.dev = true
		e.botStatePath = env.Args[0]
	}

	// Initialize internal state.
	if err := e.init.Get(func() error {
		return e.doInit(ctx)
	}); err != nil {
		return err
	}

	// Used in tests.
	if e.noServerStart {
		return nil
	}

	serverLogger := e.logger.WithGroup("server")

	// If running on Render, try to look up port to listen on and start goroutine that prevents Starlet from sleeping.
	if e.onRender {
		serverLogger.Info("Running on Render.")
		// https://docs.render.com/environment-variables#all-runtimes-1
		if port := env.Getenv("PORT"); port != "" {
			e.srv.Addr = ":" + port
		}
		go e.renderSelfPing(ctx, selfPingInterval)
	}

	if e.pingURL != "" {
		go e.ping(ctx, selfPingInterval)
	}

	// If running in production mode, set the webhook in Telegram Bot API.
	if !e.dev {
		if err := e.setWebhook(ctx); err != nil {
			return err
		}
		serverLogger.Info("Running in production mode.")
	} else {
		serverLogger.Info("Running in development mode.")
	}

	// Gross hack: re-route all output from web.Server to our slog logger.
	srvCtx := cli.WithEnv(ctx, &cli.Env{
		Getenv: env.Getenv,
		Stderr: &slogWriter{serverLogger},
	})
	return e.srv.ListenAndServe(srvCtx)
}

type slogWriter struct{ log *slog.Logger }

func (sw *slogWriter) Write(p []byte) (n int, err error) {
	// Trim trailing newlines, as slog handlers add their own.
	msg := string(bytes.TrimSpace(p))
	// Don't log empty messages.
	if msg != "" {
		sw.log.Info(msg)
	}
	return len(p), nil
}

func parseInt(s string) int64 {
	i, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		return i
	}
	return 0
}

type engine struct {
	init syncx.Lazy[error] // main initialization

	// initialized by doInit
	bot           *bot.Bot
	gistc         *gist.Client
	logStream     logstream.Streamer
	tgInterceptor *tgInterceptor
	logger        *slog.Logger
	mux           *http.ServeMux
	scrubber      *strings.Replacer
	srv           *web.Server
	tgAuth        *tgauth.Middleware

	// configuration, read-only after initialization
	addr             string
	botStatePath     string
	dev              bool
	geminiKey        string
	geminiProxyToken string
	ghToken          string
	gistID           string
	host             string
	httpc            *http.Client
	onRender         bool
	pingURL          string
	reloadToken      string
	stderr           io.Writer
	tgOwner          int64
	tgSecret         string
	tgToken          string
	// for tests
	noServerStart bool
	ready         func() // see web.Server.Ready
}

const (
	authSessionTTL   = 24 * time.Hour
	kvCacheTTL       = 24 * time.Hour
	selfPingInterval = 10 * time.Minute
)

func (e *engine) doInit(ctx context.Context) error {
	if e.stderr == nil {
		e.stderr = os.Stderr
	}

	const logLineLimit = 300
	e.logStream = logstream.New(logLineLimit)
	wr := io.MultiWriter(e.logStream, e.stderr)
	var h slog.Handler
	if !e.dev {
		h = slog.NewJSONHandler(wr, &slog.HandlerOptions{})
	} else {
		h = tint.NewHandler(wr, &tint.Options{})
	}
	e.logger = slog.New(h)

	if e.httpc == nil {
		e.httpc = &http.Client{
			// Increase timeout to properly handle Gemini API response times.
			Timeout: 60 * time.Second,
		}
	}
	if e.dev {
		tr := http.DefaultTransport
		if e.httpc.Transport != nil {
			tr = e.httpc.Transport
		}
		e.tgInterceptor = newTgInterceptor(e.logger, tr)
		e.httpc.Transport = e.tgInterceptor
	}

	var scrubPairs []string
	for _, val := range []string{
		e.ghToken,
		e.gistID,
		e.tgSecret,
		e.tgToken,
		e.geminiKey,
	} {
		if val != "" {
			scrubPairs = append(scrubPairs, val, "[EXPUNGED]")
		}
	}
	if len(scrubPairs) > 0 {
		e.scrubber = strings.NewReplacer(scrubPairs...)
	}

	e.gistc = &gist.Client{
		Token:      e.ghToken,
		HTTPClient: e.httpc,
		Scrubber:   e.scrubber,
	}
	e.tgAuth = &tgauth.Middleware{
		CheckFunc: e.authCheck,
		Token:     e.tgToken,
		TTL:       authSessionTTL,
	}

	opts := bot.Opts{
		Token:      e.tgToken,
		Secret:     e.tgSecret,
		Owner:      e.tgOwner,
		IsDev:      e.dev,
		HTTPClient: e.httpc,
		KVCache:    kvcache.Module(ctx, kvCacheTTL),
		Scrubber:   e.scrubber,
		Logger:     e.logger.WithGroup("bot"),
	}

	if e.geminiKey != "" {
		opts.GeminiClient = &gemini.Client{
			APIKey:     e.geminiKey,
			HTTPClient: e.httpc,
			Scrubber:   e.scrubber,
		}
	}

	if e.dev {
		opts.BotID = 123456789
		opts.BotUsername = "test_bot"
	} else {
		me, err := e.getMe(ctx)
		if err != nil {
			return err
		}
		opts.BotID = me.Result.ID
		opts.BotUsername = me.Result.Username
	}

	e.bot = bot.New(opts)

	if e.dev {
		if err := e.loadFromTxtar(ctx); err != nil {
			return err
		}
	} else {
		if err := e.loadFromGist(ctx); err != nil {
			return err
		}
	}

	e.initRoutes()
	e.srv = &web.Server{
		Addr:       e.addr,
		Debuggable: true, // debug endpoints protected by Telegram auth
		Mux:        e.mux,
		Ready:      e.ready,
		StaticFS:   staticFS,
		Middleware: []web.Middleware{
			e.tgAuth.Middleware(false),
			e.debugAuth,
		},
	}

	return nil
}

func (e *engine) getMe(ctx context.Context) (getMeResponse, error) {
	return request.Make[getMeResponse](ctx, request.Params{
		Method:     http.MethodGet,
		URL:        tgAPI + "/bot" + e.tgToken + "/getMe",
		HTTPClient: e.httpc,
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
		Scrubber: e.scrubber,
	})
}

type getMeResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		ID                      int64  `json:"id"`
		IsBot                   bool   `json:"is_bot"`
		FirstName               string `json:"first_name"`
		Username                string `json:"username"`
		CanJoinGroups           bool   `json:"can_join_groups"`
		CanReadAllGroupMessages bool   `json:"can_read_all_group_messages"`
		SupportsInlineQueries   bool   `json:"supports_inline_queries"`
		CanConnectToBusiness    bool   `json:"can_connect_to_business"`
		HasMainWebApp           bool   `json:"has_main_web_app"`
	} `json:"result"`
}

func (e *engine) debugAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e.tgAuth.LoggedIn(r) {
			r = web.TrustRequest(r)
			next.ServeHTTP(w, r)
			return
		}

		if !strings.HasPrefix(r.URL.Path, "/debug") || e.dev {
			next.ServeHTTP(w, r)
			return
		}

		web.RespondError(w, r, web.ErrUnauthorized)
	})
}

func (e *engine) authCheck(_ *http.Request, ident *tgauth.Identity) bool {
	return ident.ID == e.tgOwner
}

func (e *engine) loadFromGist(ctx context.Context) error {
	if e.dev {
		return errors.New("cannot load from gist in development mode")
	}
	g, err := e.gistc.Get(ctx, e.gistID)
	if err != nil {
		return err
	}
	files := make(map[string]string)
	for name, file := range g.Files {
		files[name] = file.Content
	}

	return e.bot.Load(ctx, files)
}

func (e *engine) loadFromTxtar(ctx context.Context) error {
	if !e.dev {
		return errors.New("cannot load from txtar in production mode")
	}
	ar, err := txtar.ParseFile(e.botStatePath)
	if err != nil {
		return err
	}
	files := make(map[string]string)
	for _, f := range ar.Files {
		files[f.Name] = string(f.Data)
	}
	return e.bot.Load(ctx, files)
}

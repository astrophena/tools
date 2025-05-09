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
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/request"
	"go.astrophena.name/base/syncx"
	"go.astrophena.name/base/tgauth"
	"go.astrophena.name/base/version"
	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/internal/api/github/gist"
	"go.astrophena.name/tools/internal/api/google/gemini"
	"go.astrophena.name/tools/internal/starlark/interpreter"
	"go.astrophena.name/tools/internal/starlark/lib/kvcache"
	"go.astrophena.name/tools/internal/util/logstream"

	"go.starlark.net/starlarkstruct"
)

const tgAPI = "https://api.telegram.org"

func main() { cli.Main(new(engine)) }

func (e *engine) Flags(fs *flag.FlagSet) {
	fs.Int64Var(&e.tgOwner, "tg-owner", 0, "Telegram user `ID` of the bot owner.")
	fs.StringVar(&e.addr, "addr", "localhost:3000", "Listen on `host:port`.")
	fs.StringVar(&e.geminiKey, "gemini-key", "", "Gemini API `key`.")
	fs.StringVar(&e.ghToken, "gh-token", "", "GitHub API `token`.")
	fs.StringVar(&e.gistID, "gist-id", "", "GitHub Gist `ID` to load bot code from.")
	fs.StringVar(&e.host, "host", "", "Bot `domain` used for setting up webhook.")
	fs.StringVar(&e.reloadToken, "reload-token", "", "Secret `token` used for calling /reload endpoint.")
	fs.StringVar(&e.tgSecret, "tg-secret", "", "Secret `token` used to validate Telegram Bot API updates.")
	fs.StringVar(&e.tgToken, "tg-token", "", "Telegram Bot API `token`.")
}

func (e *engine) Run(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	// Load configuration from environment variables or flags.
	e.geminiKey = cmp.Or(env.Getenv("GEMINI_KEY"), e.geminiKey)
	e.ghToken = cmp.Or(env.Getenv("GH_TOKEN"), e.ghToken)
	e.gistID = cmp.Or(env.Getenv("GIST_ID"), e.gistID)
	e.host = cmp.Or(env.Getenv("HOST"), e.host)
	e.onRender = env.Getenv("RENDER") == "true"
	e.reloadToken = cmp.Or(env.Getenv("RELOAD_TOKEN"), e.reloadToken)
	e.tgOwner = cmp.Or(parseInt(env.Getenv("TG_OWNER")), e.tgOwner)
	e.tgSecret = cmp.Or(env.Getenv("TG_SECRET"), e.tgSecret)
	e.tgToken = cmp.Or(env.Getenv("TG_TOKEN"), e.tgToken)

	e.stderr = env.Stderr

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

	// If running on Render, try to look up port to listen on, activate webhook
	// and start goroutine that prevents Starlet from sleeping.
	if e.onRender {
		env.Logf("Running on Render: starting self-ping goroutine.")
		// https://docs.render.com/environment-variables#all-runtimes-1
		if port := env.Getenv("PORT"); port != "" {
			e.addr = ":" + port
		}
		if err := e.setWebhook(ctx); err != nil {
			return err
		}
		go e.selfPing(ctx, selfPingInterval)
	}

	return e.srv.ListenAndServe(ctx)
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
	geminic   *gemini.Client
	gistc     *gist.Client
	kvCache   *starlarkstruct.Module
	logStream logstream.Streamer
	logf      logger.Logf
	mux       *http.ServeMux
	scrubber  *strings.Replacer
	srv       *web.Server
	tgAuth    *tgauth.Middleware

	// configuration, read-only after initialization
	addr          string
	geminiKey     string
	ghToken       string
	gistID        string
	host          string
	httpc         *http.Client
	me            *getMeResponse // obtained from Telegram Bot API
	onRender      bool
	reloadToken   string
	stderr        io.Writer
	tgBotID       int64
	tgBotUsername string
	tgOwner       int64
	tgSecret      string
	tgToken       string
	// for tests
	noServerStart bool
	ready         func() // see web.Server.Ready

	bot atomic.Pointer[bot] // loaded from Gist
}

// bot represents a currently running instance of the bot. It's immutable.
type bot struct {
	errorTemplate string                   // error.tmpl from Gist or default one
	files         map[string]string        // Gist files
	intr          *interpreter.Interpreter // holds and executes Starlark code
}

const (
	authSessionTTL   = 24 * time.Hour
	kvCacheTTL       = 24 * time.Hour
	selfPingInterval = 10 * time.Minute
)

func (e *engine) doInit(ctx context.Context) error {
	if e.httpc == nil {
		e.httpc = &http.Client{
			// Increase timeout to properly handle Gemini API response times.
			Timeout: 60 * time.Second,
		}
	}
	if e.stderr == nil {
		e.stderr = os.Stderr
	}

	e.kvCache = kvcache.Module(kvCacheTTL)

	const logLineLimit = 300
	e.logStream = logstream.New(logLineLimit)
	e.logf = log.New(io.MultiWriter(e.stderr, &timestampWriter{e.logStream}), "", 0).Printf

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
	if e.geminiKey != "" {
		e.geminic = &gemini.Client{
			APIKey:     e.geminiKey,
			HTTPClient: e.httpc,
			Scrubber:   e.scrubber,
		}
	}
	e.tgAuth = &tgauth.Middleware{
		CheckFunc: e.authCheck,
		Token:     e.tgToken,
		TTL:       authSessionTTL,
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

	if err := e.loadFromGist(ctx); err != nil {
		return err
	}

	me, err := e.getMe(ctx)
	if err != nil {
		return err
	}
	e.me = &me
	e.tgBotID = me.Result.ID
	e.tgBotUsername = me.Result.Username

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

// timestampWriter is an io.Writer that prefixes each line with the current date and time.
type timestampWriter struct {
	w io.Writer
}

// Write implements the [io.Writer] interface.
func (tw *timestampWriter) Write(p []byte) (n int, err error) {
	lines := bytes.SplitAfter(p, []byte{'\n'})

	for _, line := range lines {
		if len(line) > 0 {
			timestamp := time.Now().Format(time.DateTime + "\t")
			_, err := tw.w.Write([]byte(timestamp))
			if err != nil {
				return n, err
			}
			nn, err := tw.w.Write(line)
			n += nn
			if err != nil {
				return n, err
			}
		}
	}

	return n, nil
}

func (e *engine) debugAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e.tgAuth.LoggedIn(r) {
			r = web.TrustRequest(r)
			next.ServeHTTP(w, r)
			return
		}

		if !strings.HasPrefix(r.URL.Path, "/debug") || e.onRender {
			next.ServeHTTP(w, r)
			return
		}

		web.RespondError(w, r, web.ErrUnauthorized)
	})
}

func (e *engine) authCheck(_ *http.Request, ident *tgauth.Identity) bool {
	return ident.ID == e.tgOwner
}

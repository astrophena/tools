// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// vim: foldmethod=marker

package main

import (
	"cmp"
	"context"
	_ "embed"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/request"
	"go.astrophena.name/base/syncx"
	"go.astrophena.name/base/version"
	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/cmd/starlet/internal/bot"
	"go.astrophena.name/tools/cmd/starlet/internal/logstream"
	"go.astrophena.name/tools/internal/api/gemini"
	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/starlark/kvcache"
	"go.astrophena.name/tools/internal/store"

	"golang.org/x/sync/errgroup"
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
	e.adminAddr = cmp.Or(e.adminAddr, env.Getenv("ADMIN_ADDR"))
	e.databasePath = cmp.Or(e.databasePath, env.Getenv("DATABASE_PATH"))
	e.geminiKey = cmp.Or(e.geminiKey, env.Getenv("GEMINI_KEY"))
	e.ghToken = cmp.Or(e.ghToken, env.Getenv("GH_TOKEN"))
	e.gistID = cmp.Or(e.gistID, env.Getenv("GIST_ID"))
	e.host = cmp.Or(e.host, env.Getenv("HOST"))
	e.pingURL = cmp.Or(e.pingURL, env.Getenv("PING_URL"))
	e.tgOwner = cmp.Or(e.tgOwner, parseInt(env.Getenv("TG_OWNER")))
	e.tgSecret = cmp.Or(e.tgSecret, env.Getenv("TG_SECRET"))
	e.tgToken = cmp.Or(e.tgToken, env.Getenv("TG_TOKEN"))

	// Initialize internal state.
	if err := e.init.Get(func() error {
		return e.doInit(ctx)
	}); err != nil {
		return err
	}
	if e.store != nil {
		defer e.store.Close()
	}

	// Used in tests.
	if e.noServerStart {
		return nil
	}

	if e.pingURL != "" {
		go e.ping(ctx, selfPingInterval)
	}

	if err := e.setWebhook(ctx); err != nil {
		return err
	}

	sdNotify(ctx, sdNotifyReady)
	interval := watchdogInterval(env)
	if interval > 0 {
		go func() {
			ticker := time.NewTicker(interval / 2)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					sdNotify(ctx, sdNotifyWatchdog)
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return e.srv.ListenAndServe(ctx)
	})

	if e.adminAddr != "" {
		adminSrv := &web.Server{
			Addr:       e.adminAddr,
			Mux:        e.adminMux,
			StaticFS:   staticFS,
			CSP:        e.cspMux,
			Debuggable: true,
		}
		g.Go(func() error {
			return adminSrv.ListenAndServe(ctx)
		})
	}

	return g.Wait()
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
	bot       *bot.Bot
	cspMux    *web.CSPMux
	gistc     *gist.Client
	logStream logstream.Streamer
	logger    *slog.Logger
	mux       *http.ServeMux
	adminMux  *http.ServeMux
	scrubber  *strings.Replacer
	srv       *web.Server
	store     store.Store

	// configuration, read-only after initialization
	addr         string
	adminAddr    string
	databasePath string
	geminiKey    string
	ghToken      string
	gistID       string
	host         string
	httpc        *http.Client
	pingURL      string
	tgOwner      int64
	tgSecret     string
	tgToken      string
	// for tests
	noServerStart bool
	ready         func() // see web.Server.Ready
}

const (
	kvCacheTTL       = 24 * time.Hour
	selfPingInterval = 10 * time.Minute
)

func (e *engine) doInit(ctx context.Context) error {
	const logLineLimit = 300
	e.logStream = logstream.New(logLineLimit)

	logger := logger.Get(ctx)
	logger.Attach(slog.NewJSONHandler(e.logStream, &slog.HandlerOptions{
		Level: logger.Level,
	}))
	e.logger = logger.Logger

	if e.httpc == nil {
		e.httpc = &http.Client{
			// Increase timeout to properly handle Gemini API response times.
			Timeout: 60 * time.Second,
		}
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

	if e.databasePath != "" {
		s, err := store.NewSQLiteStore(ctx, e.databasePath, kvCacheTTL)
		if err != nil {
			return err
		}
		e.store = s
	} else {
		e.store = store.NewMemStore(ctx, kvCacheTTL)
	}

	opts := bot.Opts{
		Token:      e.tgToken,
		Secret:     e.tgSecret,
		Owner:      e.tgOwner,
		HTTPClient: e.httpc,
		KVCache:    kvcache.Module(ctx, e.store),
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

	me, err := e.getMe(ctx)
	if err != nil {
		return err
	}
	opts.BotID = me.Result.ID
	opts.BotUsername = me.Result.Username

	e.bot = bot.New(opts)

	if err := e.loadFromGist(ctx); err != nil {
		return err
	}

	e.cspMux = web.NewCSPMux()

	e.initRoutes()
	e.srv = &web.Server{
		Addr:     e.addr,
		Mux:      e.mux,
		Ready:    e.ready,
		StaticFS: staticFS,
		CSP:      e.cspMux,
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

func (e *engine) loadFromGist(ctx context.Context) error {
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

const (
	// sdNotifyReady tells the service manager that service startup is
	// finished, or the service finished loading its configuration.
	// https://www.freedesktop.org/software/systemd/man/sd_notify.html#READY=1
	sdNotifyReady = "READY=1"

	// sdNotifyWatchdog the service manager to update the watchdog timestamp.
	// https://www.freedesktop.org/software/systemd/man/sd_notify.html#WATCHDOG=1
	sdNotifyWatchdog = "WATCHDOG=1"
)

// watchdogInterval returns the watchdog interval configured in systemd unit file.
func watchdogInterval(env *cli.Env) time.Duration {
	s, err := strconv.Atoi(env.Getenv("WATCHDOG_USEC"))
	if err != nil {
		return 0
	}
	if s <= 0 {
		return 0
	}
	return time.Duration(s) * time.Microsecond
}

// sdNotify sends a message to systemd using the sd_notify protocol.
// See https://www.freedesktop.org/software/systemd/man/sd_notify.html.
func sdNotify(ctx context.Context, state string) {
	addr := &net.UnixAddr{
		Net:  "unixgram",
		Name: cli.GetEnv(ctx).Getenv("NOTIFY_SOCKET"),
	}

	if addr.Name == "" {
		// We're not running under systemd (NOTIFY_SOCKET is not set).
		return
	}

	conn, err := net.DialUnix(addr.Net, nil, addr)
	if err != nil {
		logger.Error(ctx, "sdnotify failed", slog.String("state", state), slog.Any("err", err))
	}
	defer conn.Close()

	if _, err = conn.Write([]byte(state)); err != nil {
		logger.Error(ctx, "sdnotify failed", slog.String("state", state), slog.Any("err", err))
	}
}

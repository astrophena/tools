// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"cmp"
	"context"
	_ "embed"
	"log/slog"
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
	"go.astrophena.name/base/web/service"
	"go.astrophena.name/tools/cmd/starlet/internal/bot"
	"go.astrophena.name/tools/internal/api/gemini"
	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/starlark/kvcache"
	"go.astrophena.name/tools/internal/store"
)

const tgAPI = "https://api.telegram.org"

func main() { service.Run(new(engine)) }

type engine struct {
	init syncx.Lazy[error] // main initialization

	// initialized by doInit
	bot      *bot.Bot
	cspMux   *web.CSPMux
	gistc    *gist.Client
	logger   *slog.Logger
	mux      *http.ServeMux
	adminMux *http.ServeMux
	scrubber *strings.Replacer
	srv      *web.Server
	store    store.Store

	// configuration, read-only after initialization
	databasePath string
	geminiKey    string
	ghToken      string
	gistID       string
	host         string
	httpc        *http.Client
	tgOwner      int64
	tgSecret     string
	tgToken      string
}

func (e *engine) PublicEndpoint(ctx context.Context) (*service.EndpointConfig, error) {
	return e.endpointConfig(ctx, false)
}

func (e *engine) AdminEndpoint(ctx context.Context) (*service.EndpointConfig, error) {
	return e.endpointConfig(ctx, true)
}

func (e *engine) endpointConfig(ctx context.Context, admin bool) (*service.EndpointConfig, error) {
	if err := e.init.Get(func() error {
		return e.doInit(ctx)
	}); err != nil {
		return nil, err
	}
	ec := &service.EndpointConfig{
		Mux:      e.mux,
		StaticFS: staticFS,
		CSP:      e.cspMux,
	}
	if admin {
		ec.Mux = e.adminMux
	}
	return ec, nil
}

func (e *engine) Shutdown(ctx context.Context) error { return e.store.Close() }

func (e *engine) doInit(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	// Load configuration from environment variables.
	e.databasePath = cmp.Or(e.databasePath, env.Getenv("DATABASE_PATH"))
	e.geminiKey = cmp.Or(e.geminiKey, env.Getenv("GEMINI_KEY"))
	e.ghToken = cmp.Or(e.ghToken, env.Getenv("GH_TOKEN"))
	e.gistID = cmp.Or(e.gistID, env.Getenv("GIST_ID"))
	e.host = cmp.Or(e.host, env.Getenv("HOST"))
	e.tgOwner = cmp.Or(e.tgOwner, parseInt(env.Getenv("TG_OWNER")))
	e.tgSecret = cmp.Or(e.tgSecret, env.Getenv("TG_SECRET"))
	e.tgToken = cmp.Or(e.tgToken, env.Getenv("TG_TOKEN"))

	logger := logger.Get(ctx)
	e.logger = logger.Logger

	if e.httpc == nil {
		e.httpc = &http.Client{
			// Increase timeout to properly handle Gemini API response times.
			Timeout: 60 * time.Second,
		}
	}

	scrubPairs := make([]string, 0, 10)
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

	const kvCacheTTL = 24 * time.Hour
	if e.databasePath != "" {
		s, err := store.NewJSONFile(ctx, e.databasePath, kvCacheTTL)
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
	if err := e.setWebhook(ctx); err != nil {
		return err
	}

	e.cspMux = web.NewCSPMux()
	e.initRoutes()

	return nil
}

func parseInt(s string) int64 {
	i, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		return i
	}
	return 0
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
	files := make(map[string]string, len(g.Files))
	for name, file := range g.Files {
		files[name] = file.Content
	}

	return e.bot.Load(ctx, files)
}

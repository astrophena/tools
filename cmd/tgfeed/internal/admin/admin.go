// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:generate deno run -A build.ts

// Package admin implements the administrative web UI and API for tgfeed.
package admin

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"slices"
	"strconv"
	"text/template"

	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/cmd/tgfeed/internal/state"
	"go.astrophena.name/tools/cmd/tgfeed/internal/stats"
)

var errConflict = web.StatusErr(http.StatusConflict)

// Config configures the tgfeed admin HTTP API.
type Config struct {
	// Addr is the network address passed to [web.Server].
	Addr string
	// StateDir is tgfeed's local state directory.
	StateDir string
	// Store reads and writes tgfeed persisted state.
	Store state.Store
	// ValidateConfig validates a Starlark config before persisting it.
	ValidateConfig func(ctx context.Context, content string) error
	// IsRunLocked reports whether tgfeed run lock is currently held.
	IsRunLocked func() bool
	// StatsStore reads persisted tgfeed run stats.
	StatsStore *stats.Store
}

// Handler returns an HTTP handler serving the tgfeed admin API.
func Handler(cfg Config) (*http.ServeMux, error) {
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	api := newAPI(cfg)

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !slices.Contains([]string{"/", "/stats", "/config", "/configuration"}, r.URL.Path) {
			web.RespondJSONError(w, r, web.ErrNotFound)
			return
		}
		var buf bytes.Buffer
		if err := appTemplate.Execute(&buf, struct {
			JS   string
			CSS  string
			Icon string
			Logo string
		}{
			JS:   web.StaticHashName(r.Context(), "static/js/app.min.js"),
			CSS:  web.StaticHashName(r.Context(), "static/css/app.min.css"),
			Icon: web.StaticHashName(r.Context(), "static/icons/icon.webp"),
			Logo: web.StaticHashName(r.Context(), "static/icons/logo.webp"),
		}); err != nil {
			web.RespondError(w, r, err)
			return
		}
		buf.WriteTo(w)
	})

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			api.handleGetConfig(w, r)
		case http.MethodPut:
			api.handlePutConfig(w, r)
		default:
			web.RespondJSONError(w, r, fmt.Errorf("%w: method not allowed", web.ErrMethodNotAllowed))
		}
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			api.handleGetState(w, r)
		case http.MethodPut:
			api.handlePutState(w, r)
		default:
			web.RespondJSONError(w, r, fmt.Errorf("%w: method not allowed", web.ErrMethodNotAllowed))
		}
	})
	mux.HandleFunc("/api/error-template", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			api.handleGetErrorTemplate(w, r)
		case http.MethodPut:
			api.handlePutErrorTemplate(w, r)
		default:
			web.RespondJSONError(w, r, fmt.Errorf("%w: method not allowed", web.ErrMethodNotAllowed))
		}
	})
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			api.handleGetStats(w, r)
		default:
			web.RespondJSONError(w, r, fmt.Errorf("%w: method not allowed", web.ErrMethodNotAllowed))
		}
	})
	mux.HandleFunc("/api/stats/run", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			api.handleGetStatsRun(w, r)
		default:
			web.RespondJSONError(w, r, fmt.Errorf("%w: method not allowed", web.ErrMethodNotAllowed))
		}
	})

	dbg := web.Debugger(mux)
	dbg.Link("/api/config", "Config")
	dbg.Link("/api/state", "State")
	dbg.Link("/api/error-template", "Error template")
	dbg.Link("/api/stats", "Stats")
	dbg.Link("/api/stats/run", "Stats run")

	return mux, nil
}

//go:embed static/*
var staticFS embed.FS

var (
	//go:embed templates/app.tmpl
	appTemplateStr string
	appTemplate    = template.Must(template.New("").Parse(appTemplateStr))
)

// Run starts the tgfeed admin HTTP API.
func Run(ctx context.Context, cfg Config) error {
	mux, err := Handler(cfg)
	if err != nil {
		return err
	}

	csp := web.NewCSPMux()
	csp.Handle("/", web.CSP{
		DefaultSrc:           []string{web.CSPSelf},
		ScriptSrc:            []string{web.CSPSelf},
		FrameAncestors:       []string{web.CSPNone},
		FormAction:           []string{web.CSPSelf},
		BaseURI:              []string{web.CSPSelf},
		ObjectSrc:            []string{web.CSPSelf},
		BlockAllMixedContent: true,
		// Needed to allow inline CSS for CodeMirror editor.
		StyleSrc: []string{web.CSPSelf, web.CSPUnsafeInline},
	})

	srv := &web.Server{
		Mux:           mux,
		Addr:          cfg.Addr,
		CSP:           csp,
		Debuggable:    true,
		NotifySystemd: true,
		StaticFS:      staticFS,
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if idleTracker := newTracker(cancel, isSocketActivated); idleTracker != nil {
		idleTracker.run(ctx)
		srv.Middleware = append(srv.Middleware, idleTracker.handler)
	}

	return srv.ListenAndServe(ctx)
}

func normalizeConfig(cfg Config) (Config, error) {
	if cfg.Store == nil {
		return Config{}, errors.New("admin: store must not be nil")
	}
	if cfg.ValidateConfig == nil {
		cfg.ValidateConfig = func(context.Context, string) error { return nil }
	}
	if cfg.IsRunLocked == nil {
		cfg.IsRunLocked = func() bool { return false }
	}
	if cfg.StatsStore == nil {
		cfg.StatsStore = stats.OpenReader(cfg.StateDir)
		if err := cfg.StatsStore.Bootstrap(context.Background()); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

type api struct {
	store            state.Store
	validateConfigFn func(context.Context, string) error
	isRunLocked      func() bool
	statsStore       *stats.Store
}

func newAPI(cfg Config) *api {
	return &api{
		store:            cfg.Store,
		validateConfigFn: cfg.ValidateConfig,
		isRunLocked:      cfg.IsRunLocked,
		statsStore:       cfg.StatsStore,
	}
}

func (a *api) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	config, err := a.store.LoadConfig(r.Context())
	if err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to read config: %v", err))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(config))
}

func (a *api) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	if !a.guardRunUnlocked(w, r) {
		return
	}

	content, ok := readBody(w, r)
	if !ok {
		return
	}

	if err := a.validateConfig(content, r); err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	if err := a.store.SaveConfig(r.Context(), string(content)); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to write config: %v", err))
		return
	}

	writeNoContent(w)
}

func (a *api) handleGetState(w http.ResponseWriter, r *http.Request) {
	stateMap, err := a.store.LoadState(r.Context())
	if err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to read state: %v", err))
		return
	}
	content, err := state.MarshalStateMap(stateMap)
	if err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to encode state: %v", err))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(content)
}

func (a *api) handlePutState(w http.ResponseWriter, r *http.Request) {
	if !a.guardRunUnlocked(w, r) {
		return
	}

	content, ok := readBody(w, r)
	if !ok {
		return
	}

	if err := validateState(content); err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	if err := a.store.SaveStateJSON(r.Context(), content); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to write state: %v", err))
		return
	}

	writeNoContent(w)
}

func (a *api) handleGetErrorTemplate(w http.ResponseWriter, r *http.Request) {
	errorTemplate, err := a.store.LoadErrorTemplate(r.Context())
	if err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to read error template: %v", err))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(errorTemplate))
}

func (a *api) handlePutErrorTemplate(w http.ResponseWriter, r *http.Request) {
	if !a.guardRunUnlocked(w, r) {
		return
	}

	content, ok := readBody(w, r)
	if !ok {
		return
	}

	if err := a.store.SaveErrorTemplate(r.Context(), string(content)); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to write error template: %v", err))
		return
	}

	writeNoContent(w)
}

func (a *api) guardRunUnlocked(w http.ResponseWriter, r *http.Request) bool {
	if !a.isRunLocked() {
		return true
	}

	context := "cannot modify resource"
	switch r.URL.Path {
	case "/api/config":
		context = "cannot modify config"
	case "/api/state":
		context = "cannot modify state"
	case "/api/error-template":
		context = "cannot modify error template"
	}

	web.RespondJSONError(w, r, fmt.Errorf("%w: %s: run is in progress", errConflict, context))
	return false
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	content, err := io.ReadAll(r.Body)
	if err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("%w: failed to read request body", web.ErrBadRequest))
		return nil, false
	}
	return content, true
}

func writeNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) validateConfig(content []byte, r *http.Request) error {
	if err := a.validateConfigFn(r.Context(), string(content)); err != nil {
		return fmt.Errorf("%w: invalid config: %v", web.ErrBadRequest, err)
	}
	return nil
}

func validateState(content []byte) error {
	stateMap, err := state.UnmarshalStateMap(content)
	if err != nil {
		return fmt.Errorf("%w: invalid JSON: %v", web.ErrBadRequest, err)
	}
	for key, value := range stateMap {
		if value == nil {
			return fmt.Errorf("%w: invalid JSON: state entry %q must be an object", web.ErrBadRequest, key)
		}
	}
	return nil
}

func (a *api) handleGetStats(w http.ResponseWriter, r *http.Request) {
	if a.statsStore == nil {
		web.RespondJSONError(w, r, errors.New("stats store is not configured"))
		return
	}

	limit, err := queryInt(r, "limit", 100)
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}
	beforeStartedAt, err := queryInt64Optional(r, "before_started_at")
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}
	includeDetails, err := queryBool(r, "include_details", true)
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	if includeDetails {
		var response []json.RawMessage
		if beforeStartedAt == nil {
			response, err = a.statsStore.ListRuns(r.Context(), limit)
		} else {
			response, err = a.statsStore.ListRunsBefore(r.Context(), limit, *beforeStartedAt)
		}
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "No stats available.", http.StatusNotFound)
				return
			}
			web.RespondJSONError(w, r, fmt.Errorf("reading stats from SQLite: %w", err))
			return
		}
		if len(response) == 0 {
			http.Error(w, "No stats available.", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			web.RespondJSONError(w, r, fmt.Errorf("encoding stats response: %w", err))
		}
		return
	}

	response, err := a.statsStore.ListRunSummaries(r.Context(), limit, beforeStartedAt)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "No stats available.", http.StatusNotFound)
			return
		}
		web.RespondJSONError(w, r, fmt.Errorf("reading stats from SQLite: %w", err))
		return
	}
	if len(response) == 0 {
		http.Error(w, "No stats available.", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("encoding stats response: %w", err))
	}
}

func (a *api) handleGetStatsRun(w http.ResponseWriter, r *http.Request) {
	if a.statsStore == nil {
		web.RespondJSONError(w, r, errors.New("stats store is not configured"))
		return
	}

	startedAt, err := queryInt64Required(r, "started_at_unix")
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	response, err := a.statsStore.GetRunByStartedAt(r.Context(), startedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "No stats available.", http.StatusNotFound)
			return
		}
		web.RespondJSONError(w, r, fmt.Errorf("reading stats run from SQLite: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if _, err := w.Write(response); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("writing stats run response: %w", err))
	}
}

func queryInt(r *http.Request, key string, fallback int) (int, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	result, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid %q query parameter", web.ErrBadRequest, key)
	}
	if result <= 0 {
		return 0, fmt.Errorf("%w: %q query parameter must be greater than zero", web.ErrBadRequest, key)
	}
	return result, nil
}

func queryInt64Required(r *http.Request, key string) (int64, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return 0, fmt.Errorf("%w: missing %q query parameter", web.ErrBadRequest, key)
	}
	result, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid %q query parameter", web.ErrBadRequest, key)
	}
	return result, nil
}

func queryInt64Optional(r *http.Request, key string) (*int64, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return nil, nil
	}
	result, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid %q query parameter", web.ErrBadRequest, key)
	}
	return &result, nil
}

func queryBool(r *http.Request, key string, fallback bool) (bool, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	result, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%w: invalid %q query parameter", web.ErrBadRequest, key)
	}
	return result, nil
}

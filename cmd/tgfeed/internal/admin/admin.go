// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:generate deno run -A build.ts
//go:generate go tool templ fmt .
//go:generate go tool templ generate -include-version=false

// Package admin implements the administrative web UI and API for tgfeed.
package admin

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"

	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/cmd/tgfeed/internal/state"
	"go.astrophena.name/tools/cmd/tgfeed/internal/stats"
)

var (
	errConflict      = web.StatusErr(http.StatusConflict)
	errInvalidConfig = errors.New("invalid config")
)

// Store reads and writes the persisted resources exposed by the admin API.
type Store interface {
	// LoadConfig loads tgfeed Starlark configuration.
	LoadConfig(ctx context.Context) (string, error)
	// LoadState loads persisted feed runtime state.
	LoadState(ctx context.Context) (map[string]*state.Feed, error)
	// LoadErrorTemplate loads the current error template.
	LoadErrorTemplate(ctx context.Context) (string, error)
	// SaveConfig persists tgfeed Starlark configuration.
	SaveConfig(ctx context.Context, config string) error
	// SaveStateJSON persists pre-encoded feed runtime state.
	SaveStateJSON(ctx context.Context, content []byte) error
	// SaveErrorTemplate persists the error template.
	SaveErrorTemplate(ctx context.Context, content string) error
}

// Config configures the tgfeed admin HTTP API.
type Config struct {
	// Addr is the network address passed to [web.Server].
	Addr string
	// StateDir is tgfeed's local state directory.
	StateDir string
	// Store reads and writes tgfeed persisted state.
	Store Store
	// ValidateConfig validates a Starlark config before persisting it.
	ValidateConfig func(ctx context.Context, content string) error
	// IsRunLocked reports whether tgfeed run lock is currently held.
	IsRunLocked func() bool
	// StatsStore reads persisted tgfeed run stats.
	StatsStore *stats.Store
	// StaticHashName returns a cache-busting static asset name. It defaults to
	// [web.StaticHashName].
	StaticHashName func(context.Context, string) string
}

// Handler returns an HTTP handler serving the tgfeed admin API.
func Handler(cfg Config) (*http.ServeMux, error) {
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	api := newAPI(cfg)
	ui := newUI(api, cfg.StaticHashName)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", ui.handleStats)
	mux.HandleFunc("GET /stats", ui.handleStats)
	mux.HandleFunc("GET /config", ui.handleConfiguration)
	mux.HandleFunc("POST /config", ui.handleSaveAll)
	mux.HandleFunc("POST /config/config", ui.handleSaveConfig)
	mux.HandleFunc("POST /config/error-template", ui.handleSaveErrorTemplate)

	mux.HandleFunc("GET /api/config", api.handleGetConfig)
	mux.HandleFunc("PUT /api/config", api.handlePutConfig)
	mux.HandleFunc("GET /api/state", api.handleGetState)
	mux.HandleFunc("PUT /api/state", api.handlePutState)
	mux.HandleFunc("GET /api/error-template", api.handleGetErrorTemplate)
	mux.HandleFunc("PUT /api/error-template", api.handlePutErrorTemplate)
	mux.HandleFunc("GET /api/stats", api.handleGetStats)
	mux.HandleFunc("GET /api/stats/run", api.handleGetStatsRun)

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
	if cfg.StaticHashName == nil {
		cfg.StaticHashName = web.StaticHashName
	}
	return cfg, nil
}

type api struct {
	store            Store
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
	w.Write([]byte(config))
}

func (a *api) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	content, ok := readBody(w, r)
	if !ok {
		return
	}
	if err := a.saveConfig(r.Context(), string(content)); err != nil {
		if errors.Is(err, errInvalidConfig) {
			err = fmt.Errorf("%w: %v", web.ErrBadRequest, err)
		}
		web.RespondJSONError(w, r, err)
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
	w.Write(content)
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
	w.Write([]byte(errorTemplate))
}

func (a *api) handlePutErrorTemplate(w http.ResponseWriter, r *http.Request) {
	content, ok := readBody(w, r)
	if !ok {
		return
	}

	if err := a.saveErrorTemplate(r.Context(), string(content)); err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	writeNoContent(w)
}

func (a *api) guardRunUnlocked(w http.ResponseWriter, r *http.Request) bool {
	if !a.isRunLocked() {
		return true
	}

	web.RespondJSONError(w, r, fmt.Errorf("%w: cannot modify state: run is in progress", errConflict))
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

func (a *api) saveConfig(ctx context.Context, content string) error {
	// Both the JSON API and SSR forms use this mutation path so locking and
	// validation cannot drift between the two transports.
	if a.isRunLocked() {
		return fmt.Errorf("%w: cannot modify config: run is in progress", errConflict)
	}
	if err := a.validateConfigFn(ctx, content); err != nil {
		return fmt.Errorf("%w: %v", errInvalidConfig, err)
	}
	if err := a.store.SaveConfig(ctx, content); err != nil {
		return fmt.Errorf("failed to write config: %v", err)
	}
	return nil
}

func (a *api) saveErrorTemplate(ctx context.Context, content string) error {
	// Keep the run-lock check beside persistence for the same reason as config:
	// callers should not need to remember a separate guard.
	if a.isRunLocked() {
		return fmt.Errorf("%w: cannot modify error template: run is in progress", errConflict)
	}
	if err := a.store.SaveErrorTemplate(ctx, content); err != nil {
		return fmt.Errorf("failed to write error template: %v", err)
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

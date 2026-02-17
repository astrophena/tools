// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:generate deno bundle frontend/app.tsx --platform=browser --minify --output=static/js/app.min.js

package admin

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"
	"time"

	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/cmd/tgfeed/internal/state"
	"go.astrophena.name/tools/internal/idle"
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
			JS  string
			CSS string
		}{
			JS:  web.StaticHashName(r.Context(), "static/js/app.min.js"),
			CSS: web.StaticHashName(r.Context(), "static/css/app.css"),
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

	dbg := web.Debugger(mux)
	dbg.Link("/api/config", "Config")
	dbg.Link("/api/state", "State")
	dbg.Link("/api/error-template", "Error template")
	dbg.Link("/api/stats", "Stats")

	return mux, nil
}

//go:embed static/*
var staticFS embed.FS

var (
	//go:embed static/app.tmpl
	appTemplateStr string
	appTemplate    = template.Must(template.New("").Parse(appTemplateStr))
)

// Run starts the tgfeed admin HTTP API.
func Run(ctx context.Context, cfg Config) error {
	mux, err := Handler(cfg)
	if err != nil {
		return err
	}
	srv := &web.Server{
		Mux:           mux,
		Addr:          cfg.Addr,
		Debuggable:    true,
		NotifySystemd: true,
		StaticFS:      staticFS,
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if idleTracker := idle.NewTracker(cancel); idleTracker != nil {
		idleTracker.Run(ctx)
		srv.Middleware = append(srv.Middleware, idleTracker.Handler)
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
	return cfg, nil
}

type api struct {
	store            state.Store
	validateConfigFn func(context.Context, string) error
	isRunLocked      func() bool
	statsDir         string
}

type statsItem struct {
	StartTime time.Time
	Raw       json.RawMessage
}

func newAPI(cfg Config) *api {
	return &api{
		store:            cfg.Store,
		validateConfigFn: cfg.ValidateConfig,
		isRunLocked:      cfg.IsRunLocked,
		statsDir:         filepath.Join(cfg.StateDir, "stats"),
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
	entries, err := os.ReadDir(a.statsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "No stats available.", http.StatusNotFound)
			return
		}
		web.RespondJSONError(w, r, fmt.Errorf("reading stats directory: %w", err))
		return
	}

	// Filter for JSON files.
	var statsFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			statsFiles = append(statsFiles, entry.Name())
		}
	}

	// Sort by filename, descending (newest first).
	slices.SortFunc(statsFiles, func(a, b string) int {
		return strings.Compare(b, a)
	})

	// Limit to the latest 100 entries.
	if len(statsFiles) > 100 {
		statsFiles = statsFiles[:100]
	}

	var allStats []statsItem
	for _, name := range statsFiles {
		path := filepath.Join(a.statsDir, name)
		b, err := os.ReadFile(path)
		if err != nil {
			web.RespondJSONError(w, r, fmt.Errorf("reading stats file %s: %w", path, err))
			return
		}

		var s struct {
			StartTime time.Time `json:"start_time"`
		}
		if err := json.Unmarshal(b, &s); err != nil {
			web.RespondJSONError(w, r, fmt.Errorf("parsing stats file %s: %w", path, err))
			return
		}
		allStats = append(allStats, statsItem{
			StartTime: s.StartTime,
			Raw:       append(json.RawMessage(nil), b...),
		})
	}

	response := make([]json.RawMessage, len(allStats))
	for i, item := range allStats {
		response[i] = item.Raw
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("encoding stats response: %w", err))
	}
}

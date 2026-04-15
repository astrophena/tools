// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:generate deno run -A build.ts

// Package admin implements the administrative web UI and API for tgfeed.
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
	"strconv"
	"strings"
	"text/template"
	"time"

	"go.astrophena.name/base/safefile"
	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/cmd/tgfeed/internal/state"
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
	mux.HandleFunc("/api/stats/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			api.handleGetStatsDetail(w, r)
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
	return cfg, nil
}

type api struct {
	store            state.Store
	validateConfigFn func(context.Context, string) error
	isRunLocked      func() bool
	statsDir         string
}

const (
	statsDefaultLimit = 20
	statsMaxLimit     = 200
	statsIndexFile    = "index.json"
	statsIndexMaxRows = 1000
)

type statsSummary struct {
	ID string `json:"id"`

	TotalFeeds       int `json:"total_feeds"`
	SuccessFeeds     int `json:"success_feeds"`
	FailedFeeds      int `json:"failed_feeds"`
	NotModifiedFeeds int `json:"not_modified_feeds"`

	StartTime        time.Time     `json:"start_time"`
	Duration         time.Duration `json:"duration"`
	TotalItemsParsed int           `json:"total_items_parsed"`

	TotalFetchTime time.Duration `json:"total_fetch_time"`
	AvgFetchTime   time.Duration `json:"avg_fetch_time"`
	FetchLatencyMS struct {
		P50 int64 `json:"p50"`
		P90 int64 `json:"p90"`
		P99 int64 `json:"p99"`
		Max int64 `json:"max"`
	} `json:"fetch_latency_ms"`
	SendLatencyMS struct {
		P50 int64 `json:"p50"`
		P90 int64 `json:"p90"`
		P99 int64 `json:"p99"`
		Max int64 `json:"max"`
	} `json:"send_latency_ms"`

	HTTP2xxCount int `json:"http_2xx_count"`
	HTTP3xxCount int `json:"http_3xx_count"`
	HTTP4xxCount int `json:"http_4xx_count"`
	HTTP5xxCount int `json:"http_5xx_count"`

	TimeoutCount      int `json:"timeout_count"`
	NetworkErrorCount int `json:"network_error_count"`
	ParseErrorCount   int `json:"parse_error_count"`

	ItemsSeenTotal       int `json:"items_seen_total"`
	ItemsKeptTotal       int `json:"items_kept_total"`
	ItemsDedupedTotal    int `json:"items_deduped_total"`
	ItemsSkippedOldTotal int `json:"items_skipped_old_total"`
	ItemsEnqueuedTotal   int `json:"items_enqueued_total"`

	MessagesAttempted int `json:"messages_attempted"`
	MessagesSent      int `json:"messages_sent"`
	MessagesFailed    int `json:"messages_failed"`

	FetchRetriesTotal       int           `json:"fetch_retries_total"`
	FeedsRetriedCount       int           `json:"feeds_retried_count"`
	BackoffSleepTotal       time.Duration `json:"backoff_sleep_total"`
	SpecialRateLimitRetries int           `json:"special_rate_limit_retries"`

	SeenItemsEntriesTotal int `json:"seen_items_entries_total"`
	SeenItemsPrunedTotal  int `json:"seen_items_pruned_total"`
	StateBytesWritten     int `json:"state_bytes_written"`

	MemoryUsage uint64 `json:"memory_usage"`
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
	limit, err := parseStatsLimit(r.URL.Query().Get("limit"))
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	if r.URL.Query().Get("view") == "full" {
		a.handleGetFullStats(w, r, limit)
		return
	}

	summaries, err := a.loadStatsSummaries(limit)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "No stats available.", http.StatusNotFound)
			return
		}
		web.RespondJSONError(w, r, err)
		return
	}

	writeJSON(w, r, summaries, "encoding stats response")
}

func (a *api) handleGetStatsDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	content, err := os.ReadFile(filepath.Join(a.statsDir, id+".json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "No stats available.", http.StatusNotFound)
			return
		}
		web.RespondJSONError(w, r, fmt.Errorf("reading stats file %q: %w", id, err))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(content)
}

func (a *api) handleGetFullStats(w http.ResponseWriter, r *http.Request, limit int) {
	summaries, err := a.loadStatsSummaries(limit)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "No stats available.", http.StatusNotFound)
			return
		}
		web.RespondJSONError(w, r, err)
		return
	}

	response := make([]json.RawMessage, 0, len(summaries))
	for _, summary := range summaries {
		content, err := os.ReadFile(filepath.Join(a.statsDir, summary.ID+".json"))
		if err != nil {
			web.RespondJSONError(w, r, fmt.Errorf("reading stats file %q: %w", summary.ID, err))
			return
		}
		response = append(response, json.RawMessage(content))
	}

	writeJSON(w, r, response, "encoding stats response")
}

func (a *api) loadStatsSummaries(limit int) ([]statsSummary, error) {
	summaries, err := a.readStatsIndex()
	if err != nil {
		return nil, err
	}
	if len(summaries) == 0 {
		return nil, fs.ErrNotExist
	}
	if limit < len(summaries) {
		summaries = summaries[:limit]
	}
	return summaries, nil
}

func (a *api) readStatsIndex() ([]statsSummary, error) {
	content, err := os.ReadFile(filepath.Join(a.statsDir, statsIndexFile))
	if err == nil {
		var summaries []statsSummary
		if err := json.Unmarshal(content, &summaries); err == nil {
			return summaries, nil
		}
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("reading stats index: %w", err)
	}

	summaries, rebuildErr := a.rebuildStatsIndex()
	if rebuildErr != nil {
		return nil, rebuildErr
	}
	if len(summaries) > 0 {
		if content, err := json.Marshal(summaries); err == nil {
			_ = safefile.WriteFile(filepath.Join(a.statsDir, statsIndexFile), content, 0o644)
		}
	}
	return summaries, nil
}

func (a *api) rebuildStatsIndex() ([]statsSummary, error) {
	entries, err := os.ReadDir(a.statsDir)
	if err != nil {
		return nil, fmt.Errorf("reading stats directory: %w", err)
	}

	statsFiles := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || entry.Name() == statsIndexFile {
			continue
		}
		statsFiles = append(statsFiles, entry.Name())
	}

	slices.SortFunc(statsFiles, func(a, b string) int {
		return strings.Compare(b, a)
	})

	summaries := make([]statsSummary, 0, len(statsFiles))
	for _, name := range statsFiles[:min(len(statsFiles), statsIndexMaxRows)] {
		content, err := os.ReadFile(filepath.Join(a.statsDir, name))
		if err != nil {
			return nil, fmt.Errorf("reading stats file %s: %w", name, err)
		}
		var summary statsSummary
		if err := json.Unmarshal(content, &summary); err != nil {
			return nil, fmt.Errorf("parsing stats file %s: %w", name, err)
		}
		summary.ID = strings.TrimSuffix(name, ".json")
		summaries = append(summaries, summary)
	}

	return summaries, nil
}

func parseStatsLimit(value string) (int, error) {
	if value == "" {
		return statsDefaultLimit, nil
	}

	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return 0, fmt.Errorf("%w: invalid stats limit %q", web.ErrBadRequest, value)
	}
	return min(limit, statsMaxLimit), nil
}

func writeJSON(w http.ResponseWriter, r *http.Request, value any, context string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("%s: %w", context, err))
	}
}

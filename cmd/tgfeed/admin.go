// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"encoding/csv"
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
	"time"

	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/internal/atomicio"
	"go.astrophena.name/tools/internal/idle"
)

var errConflict = web.StatusErr(http.StatusConflict)

func (f *fetcher) admin(ctx context.Context) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			web.RespondJSONError(w, r, web.ErrNotFound)
			return
		}
		http.Redirect(w, r, "/debug/", http.StatusFound)
	})

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			f.handleGetConfig(w, r)
		case http.MethodPut:
			f.handlePutConfig(w, r)
		default:
			web.RespondJSONError(w, r, fmt.Errorf("method not allowed: %w", web.ErrMethodNotAllowed))
		}
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			f.handleGetState(w, r)
		case http.MethodPut:
			f.handlePutState(w, r)
		default:
			web.RespondJSONError(w, r, fmt.Errorf("method not allowed: %w", web.ErrMethodNotAllowed))
		}
	})
	mux.HandleFunc("/api/error-template", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			f.handleGetErrorTemplate(w, r)
		case http.MethodPut:
			f.handlePutErrorTemplate(w, r)
		default:
			web.RespondJSONError(w, r, fmt.Errorf("method not allowed: %w", web.ErrMethodNotAllowed))
		}
	})
	mux.HandleFunc("/debug/stats.csv", f.handleStatsCSV)

	dbg := web.Debugger(mux)
	dbg.Link("/api/config", "Config")
	dbg.Link("/api/state", "State")
	dbg.Link("/api/error-template", "Error template")
	dbg.Link("/debug/stats.csv", "Download Stats (CSV)")

	srv := &web.Server{
		Mux:           mux,
		Addr:          f.adminAddr,
		Debuggable:    true,
		NotifySystemd: true,
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if idleTracker := idle.NewTracker(cancel); idleTracker != nil {
		idleTracker.Run(ctx)
		srv.Middleware = append(srv.Middleware, idleTracker.Handler)
	}

	return srv.ListenAndServe(ctx)
}

func (f *fetcher) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	respondFile(w, r, filepath.Join(f.stateDir, "config.star"), "text/plain; charset=utf-8")
}

func (f *fetcher) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	if !f.guardRunUnlocked(w, r) {
		return
	}

	content, ok := readBody(w, r)
	if !ok {
		return
	}

	if err := f.validateConfig(content, r); err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	if err := atomicio.WriteFile(filepath.Join(f.stateDir, "config.star"), content, 0o644); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to write config: %v", err))
		return
	}

	writeNoContent(w)
}

func (f *fetcher) handleGetState(w http.ResponseWriter, r *http.Request) {
	respondFile(w, r, filepath.Join(f.stateDir, "state.json"), "application/json; charset=utf-8")
}

func (f *fetcher) handlePutState(w http.ResponseWriter, r *http.Request) {
	if !f.guardRunUnlocked(w, r) {
		return
	}

	content, ok := readBody(w, r)
	if !ok {
		return
	}

	if err := f.validateState(content); err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	if err := atomicio.WriteFile(filepath.Join(f.stateDir, "state.json"), content, 0o644); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to write state: %v", err))
		return
	}

	writeNoContent(w)
}

func (f *fetcher) handleGetErrorTemplate(w http.ResponseWriter, r *http.Request) {
	respondFile(w, r, filepath.Join(f.stateDir, "error.tmpl"), "text/plain; charset=utf-8", []byte(defaultErrorTemplate))
}

func (f *fetcher) handlePutErrorTemplate(w http.ResponseWriter, r *http.Request) {
	if !f.guardRunUnlocked(w, r) {
		return
	}

	content, ok := readBody(w, r)
	if !ok {
		return
	}

	if err := atomicio.WriteFile(filepath.Join(f.stateDir, "error.tmpl"), content, 0o644); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to write error template: %v", err))
		return
	}

	writeNoContent(w)
}

func (f *fetcher) guardRunUnlocked(w http.ResponseWriter, r *http.Request) bool {
	if !f.isRunLocked() {
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

func respondFile(w http.ResponseWriter, r *http.Request, path string, contentType string, notFoundFallback ...[]byte) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && len(notFoundFallback) > 0 {
			w.Header().Set("Content-Type", contentType)
			w.Write(notFoundFallback[0])
			return
		}
		errPrefix := "failed to read file"
		switch filepath.Base(path) {
		case "config.star":
			errPrefix = "failed to read config"
		case "state.json":
			errPrefix = "failed to read state"
		case "error.tmpl":
			errPrefix = "failed to read error template"
		}
		web.RespondJSONError(w, r, fmt.Errorf("%s: %v", errPrefix, err))
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Write(content)
}

func (f *fetcher) validateConfig(content []byte, r *http.Request) error {
	if _, err := f.parseConfig(r.Context(), string(content)); err != nil {
		return fmt.Errorf("%w: invalid config: %v", web.ErrBadRequest, err)
	}
	return nil
}

func (f *fetcher) validateState(content []byte) error {
	var stateMap map[string]*feedState
	if err := json.Unmarshal(content, &stateMap); err != nil {
		return fmt.Errorf("%w: invalid JSON: %v", web.ErrBadRequest, err)
	}
	for key, value := range stateMap {
		if value == nil {
			return fmt.Errorf("%w: invalid JSON: state entry %q must be an object", web.ErrBadRequest, key)
		}
	}
	return nil
}

func (f *fetcher) handleStatsCSV(w http.ResponseWriter, r *http.Request) {
	statsDir := filepath.Join(f.stateDir, "stats")
	entries, err := os.ReadDir(statsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "No stats available.", http.StatusNotFound)
			return
		}
		web.RespondJSONError(w, r, fmt.Errorf("reading stats directory: %w", err))
		return
	}

	var allStats []stats
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(statsDir, entry.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			web.RespondJSONError(w, r, fmt.Errorf("reading stats file %s: %w", path, err))
			return
		}

		var s stats
		if err := json.Unmarshal(b, &s); err != nil {
			web.RespondJSONError(w, r, fmt.Errorf("parsing stats file %s: %w", path, err))
			return
		}
		allStats = append(allStats, s)
	}

	// Sort by start time, newest first.
	slices.SortFunc(allStats, func(a, b stats) int {
		return b.StartTime.Compare(a.StartTime)
	})

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="stats.csv"`)

	csvWriter := csv.NewWriter(w)
	defer csvWriter.Flush()

	header := []string{
		"StartTime", "Duration", "TotalFeeds", "SuccessFeeds",
		"FailedFeeds", "NotModifiedFeeds", "TotalItemsParsed",
		"TotalFetchTime", "AvgFetchTime", "MemoryUsage (Bytes)",
	}
	if err := csvWriter.Write(header); err != nil {
		f.slog.Error("failed to write CSV header", "err", err)
		return
	}

	for _, s := range allStats {
		record := []string{
			s.StartTime.Format(time.RFC3339),
			s.Duration.String(),
			strconv.Itoa(s.TotalFeeds),
			strconv.Itoa(s.SuccessFeeds),
			strconv.Itoa(s.FailedFeeds),
			strconv.Itoa(s.NotModifiedFeeds),
			strconv.Itoa(s.TotalItemsParsed),
			s.TotalFetchTime.String(),
			s.AvgFetchTime.String(),
			strconv.FormatUint(s.MemoryUsage, 10),
		}
		if err := csvWriter.Write(record); err != nil {
			f.slog.Error("failed to write CSV record", "err", err)
			return
		}
	}
}

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

	return srv.ListenAndServe(ctx)
}

func (f *fetcher) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	content, err := os.ReadFile(filepath.Join(f.stateDir, "config.star"))
	if err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to read config: %v", err))
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(content)
}

func (f *fetcher) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	if f.isRunLocked() {
		web.RespondJSONError(w, r, fmt.Errorf("%w: cannot modify config: run is in progress", errConflict))
		return
	}

	content, err := io.ReadAll(r.Body)
	if err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("%w: failed to read request body", web.ErrBadRequest))
		return
	}

	// Validate config by parsing.
	if _, err := f.parseConfig(r.Context(), string(content)); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("%w: invalid config: %v", web.ErrBadRequest, err))
		return
	}

	if err := os.WriteFile(filepath.Join(f.stateDir, "config.star"), content, 0o644); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to write config: %v", err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (f *fetcher) handleGetState(w http.ResponseWriter, r *http.Request) {
	content, err := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
	if err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to read state: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(content)
}

func (f *fetcher) handlePutState(w http.ResponseWriter, r *http.Request) {
	if f.isRunLocked() {
		web.RespondJSONError(w, r, fmt.Errorf("%w: cannot modify state: run is in progress", errConflict))
		return
	}

	content, err := io.ReadAll(r.Body)
	if err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("%w: failed to read request body", web.ErrBadRequest))
		return
	}

	var stateMap map[string]*feedState
	if err := json.Unmarshal(content, &stateMap); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("%w: invalid JSON: %v", web.ErrBadRequest, err))
		return
	}

	if err := os.WriteFile(filepath.Join(f.stateDir, "state.json"), content, 0o644); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to write state: %v", err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (f *fetcher) handleGetErrorTemplate(w http.ResponseWriter, r *http.Request) {
	content, err := os.ReadFile(filepath.Join(f.stateDir, "error.tmpl"))
	if err != nil {
		// If file doesn't exist, return default template.
		if errors.Is(err, fs.ErrNotExist) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte(defaultErrorTemplate))
			return
		}
		web.RespondJSONError(w, r, fmt.Errorf("failed to read error template: %v", err))
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(content)
}

func (f *fetcher) handlePutErrorTemplate(w http.ResponseWriter, r *http.Request) {
	if f.isRunLocked() {
		web.RespondJSONError(w, r, fmt.Errorf("%w: cannot modify error template: run is in progress", errConflict))
		return
	}

	content, err := io.ReadAll(r.Body)
	if err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to read request body: %w", web.ErrBadRequest))
		return
	}

	if err := os.WriteFile(filepath.Join(f.stateDir, "error.tmpl"), content, 0o644); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to write error template: %v", err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
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

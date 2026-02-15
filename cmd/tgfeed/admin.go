// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
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

	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/cmd/tgfeed/internal/state"
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
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			f.handleGetStats(w, r)
		default:
			web.RespondJSONError(w, r, fmt.Errorf("method not allowed: %w", web.ErrMethodNotAllowed))
		}
	})

	dbg := web.Debugger(mux)
	dbg.Link("/api/config", "Config")
	dbg.Link("/api/state", "State")
	dbg.Link("/api/error-template", "Error template")
	dbg.Link("/api/stats", "Stats")

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
	config, err := f.store.LoadConfig(r.Context())
	if err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to read config: %v", err))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(config))
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

	if err := f.store.SaveConfig(r.Context(), string(content)); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to write config: %v", err))
		return
	}

	writeNoContent(w)
}

func (f *fetcher) handleGetState(w http.ResponseWriter, r *http.Request) {
	stateMap, err := f.store.LoadState(r.Context())
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

	if _, err := state.UnmarshalStateMap(content); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to parse state: %v", err))
		return
	}

	if err := f.store.SaveStateJSON(r.Context(), content); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to write state: %v", err))
		return
	}

	writeNoContent(w)
}

func (f *fetcher) handleGetErrorTemplate(w http.ResponseWriter, r *http.Request) {
	errorTemplate, err := f.store.LoadErrorTemplate(r.Context())
	if err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("failed to read error template: %v", err))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(errorTemplate))
}

func (f *fetcher) handlePutErrorTemplate(w http.ResponseWriter, r *http.Request) {
	if !f.guardRunUnlocked(w, r) {
		return
	}

	content, ok := readBody(w, r)
	if !ok {
		return
	}

	if err := f.store.SaveErrorTemplate(r.Context(), string(content)); err != nil {
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

func (f *fetcher) validateConfig(content []byte, r *http.Request) error {
	if _, err := f.parseConfig(r.Context(), string(content)); err != nil {
		return fmt.Errorf("%w: invalid config: %v", web.ErrBadRequest, err)
	}
	return nil
}

func (f *fetcher) validateState(content []byte) error {
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

func (f *fetcher) handleGetStats(w http.ResponseWriter, r *http.Request) {
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

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(allStats); err != nil {
		web.RespondJSONError(w, r, fmt.Errorf("encoding stats response: %w", err))
	}
}

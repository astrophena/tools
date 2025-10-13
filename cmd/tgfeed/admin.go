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

	dbg := web.Debugger(mux)
	dbg.Link("/api/config", "Config")
	dbg.Link("/api/state", "State")
	dbg.Link("/api/error-template", "Error template")
	if f.statsSpreadsheetID != "" {
		dbg.Link(fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s/view", f.statsSpreadsheetID), "Stats")
	}

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

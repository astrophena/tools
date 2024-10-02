// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package web

import (
	"net/http"
	"net/url"
	"sync"
)

// Health returns the HealthHandler registered on mux at /health, creating it
// if necessary.
func Health(mux *http.ServeMux) *HealthHandler {
	h, pat := mux.Handler(&http.Request{URL: &url.URL{Path: "/health"}})
	if hh, ok := h.(*HealthHandler); ok && pat == "/health" {
		return hh
	}
	ret := &HealthHandler{checks: make(map[string]HealthFunc)}
	mux.Handle("/health", ret)
	return ret
}

// HealthHandler is an HTTP handler that returns information about the health
// status of the running service.
type HealthHandler struct {
	mu     sync.RWMutex
	checks map[string]HealthFunc
}

// HealthFunc is the health check function that reports the state of a
// particular subsystem.
type HealthFunc func() (status string, ok bool)

// RegisterFunc registers the health check function by the given name. If the
// health check function with this name already exists, RegisterFunc panics.
//
// Health check function must be safe for concurrent use.
func (h *HealthHandler) RegisterFunc(name string, f HealthFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, dup := h.checks[name]
	if dup {
		panic("health: health check function with this name already exists")
	}
	h.checks[name] = f
}

// HealthResponse represents a response of the /health endpoint.
type HealthResponse struct {
	OK     bool                     `json:"ok"`
	Checks map[string]CheckResponse `json:"checks"`
}

// CheckResponse represents a status of an individual check.
type CheckResponse struct {
	Status string `json:"status"`
	OK     bool   `json:"ok"`
}

// ServeHTTP implements the [http.Handler] interface.
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	hr := &HealthResponse{
		OK:     true,
		Checks: make(map[string]CheckResponse),
	}

	for name, f := range h.checks {
		status, ok := f()
		if !ok {
			hr.OK = false
		}
		hr.Checks[name] = CheckResponse{Status: status, OK: ok}
	}

	w.Header().Set("Content-Type", "application/json")
	if hr.OK {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}

	RespondJSON(w, hr)
}

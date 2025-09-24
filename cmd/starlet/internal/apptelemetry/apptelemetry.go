// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package apptelemetry provides a simple telemetry collector for personal pet
// projects.
package apptelemetry

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"go.astrophena.name/base/web"

	_ "github.com/tailscale/sqlite"
)

//go:embed schema.sql
var schema string

type Collector struct {
	db  *sql.DB
	ttl time.Duration
}

func NewCollector(ctx context.Context, dbPath string, ttl time.Duration) (*Collector, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL;"); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON;"); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA strict=ON;"); err != nil {
		return nil, err
	}

	if _, err := db.ExecContext(ctx, schema); err != nil {
		return nil, err
	}

	c := &Collector{
		db:  db,
		ttl: ttl,
	}
	go c.cleanup(ctx)

	go func() {
		<-ctx.Done()
		c.db.Close()
	}()

	return c, nil
}

func (c *Collector) cleanup(ctx context.Context) {
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.db.ExecContext(ctx, `DELETE FROM app_telemetry_events WHERE created_at < datetime('now', '-' || ?);`, c.ttl.String())
		case <-ctx.Done():
			return
		}
	}
}

// Event is a telemetry event.
type Event struct {
	// SessionID is a unique identifier for a session.
	SessionID string `json:"session_id"`
	// AppName is the name of the application.
	AppName string `json:"app_name"`
	// AppVersion is the version of the application.
	AppVersion string `json:"app_version"`
	// OS is the operating system the application is running on.
	OS string `json:"os"`
	// Type is the type of the event.
	Type string `json:"event_type"`
	// Payload is the event payload.
	Payload any `json:"payload"`
}

func (e *Event) Validate() error {
	if e == nil {
		return errors.New("event is required")
	}
	if e.SessionID == "" {
		return errors.New("session_id is required")
	}
	if e.AppName == "" {
		return errors.New("app_name is required")
	}
	if e.AppVersion == "" {
		return errors.New("app_version is required")
	}
	if e.OS == "" {
		return errors.New("os is required")
	}
	if e.Type == "" {
		return errors.New("event_type is required")
	}
	if e.Payload == nil {
		return errors.New("payload is required")
	}
	return nil
}

type response struct {
	Status string `json:"status"`
}

func (c *Collector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle preflight request.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Max-Age", "1728000")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	if r.Method == http.MethodOptions {
		return
	}

	web.HandleJSON(func(r *http.Request, evt *Event) (*response, error) {
		payload, err := json.Marshal(evt.Payload)
		if err != nil {
			return nil, err
		}

		if _, err := c.db.ExecContext(r.Context(), `
			INSERT INTO app_telemetry_events (session_id, app_name, app_version, os, event_type, payload)
			VALUES (?, ?, ?, ?, ?, ?);
		`, evt.SessionID, evt.AppName, evt.AppVersion, evt.OS, evt.Type, payload); err != nil {
			return nil, err
		}
		return &response{Status: "success"}, nil
	}).ServeHTTP(w, r)
}

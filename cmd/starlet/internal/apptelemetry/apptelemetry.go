// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package apptelemetry

import (
	"context"
	_ "embed"
	"net/http"
	"time"

	"go.astrophena.name/base/web"

	"github.com/jackc/pgx/v5"
)

//go:embed schema.sql
var schema string

type Collector struct {
	conn *pgx.Conn
	ttl  time.Duration
}

func NewCollector(ctx context.Context, databaseURL string, ttl time.Duration) (*Collector, error) {
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return nil, err
	}

	if _, err := conn.Exec(ctx, schema); err != nil {
		return nil, err
	}

	c := &Collector{
		conn: conn,
		ttl:  ttl,
	}
	go c.cleanup(ctx)
	return c, nil
}

func (c *Collector) cleanup(ctx context.Context) {
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.conn.Exec(ctx, `DELETE FROM app_telemetry_events WHERE created_at < NOW() - $1;`, c.ttl.String())
		case <-ctx.Done():
			return
		}
	}
}

type Event struct {
	SessionID  string `json:"session_id"`
	AppName    string `json:"app_name"`
	AppVersion string `json:"app_version"`
	OS         string `json:"os"`
	Type       string `json:"event_type"`
	Payload    any    `json:"payload"`
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
	if r.Method == http.MethodOptions {
		return
	}

	web.HandleJSON(func(r *http.Request, evt *Event) (*response, error) {
		if _, err := c.conn.Exec(r.Context(), `
INSERT INTO app_telemetry_events (session_id, app_version, os, event_type, payload, created_at)
VALUES ($1, $2, $3, $4, $5, NOW());
`, evt.SessionID, evt.AppVersion, evt.OS, evt.Type, evt.Payload); err != nil {
			return nil, err
		}
		return &response{Status: "success"}, nil
	}).ServeHTTP(w, r)
}

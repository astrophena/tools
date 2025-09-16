// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package apptelemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestPostgresCollector(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set, skipping")
	}

	c, err := NewCollector(context.Background(), databaseURL, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer c.conn.Close(context.Background())

	if _, err := c.conn.Exec(context.Background(), "DELETE FROM app_telemetry_events"); err != nil {
		t.Fatal(err)
	}

	testCollector(t, c)
}

func testCollector(t *testing.T, c *Collector) {
	t.Helper()

	t.Run("OK", func(t *testing.T) {
		evt := &Event{
			SessionID:  "test-session",
			AppName:    "test-app",
			AppVersion: "1.0.0",
			OS:         "linux",
			Type:       "test-event",
			Payload:    map[string]any{"foo": "bar"},
		}

		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(evt); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodPost, "/", &buf)
		res := httptest.NewRecorder()

		c.ServeHTTP(res, req)

		if res.Code != http.StatusOK {
			t.Errorf("got status %d, want %d", res.Code, http.StatusOK)
		}

		// Check that the event was stored in the database.
		var (
			sessionID, appName, appVersion, os, eventType string
			payload                                       map[string]any
		)
		if err := c.conn.QueryRow(context.Background(), `
			SELECT session_id, app_name, app_version, os, event_type, payload
			FROM app_telemetry_events
			WHERE session_id = $1
		`, evt.SessionID).Scan(&sessionID, &appName, &appVersion, &os, &eventType, &payload); err != nil {
			t.Fatal(err)
		}

		if sessionID != evt.SessionID {
			t.Errorf("got session_id %q, want %q", sessionID, evt.SessionID)
		}
		if appName != evt.AppName {
			t.Errorf("got app_name %q, want %q", appName, evt.AppName)
		}
		if appVersion != evt.AppVersion {
			t.Errorf("got app_version %q, want %q", appVersion, evt.AppVersion)
		}
		if os != evt.OS {
			t.Errorf("got os %q, want %q", os, evt.OS)
		}
		if eventType != evt.Type {
			t.Errorf("got event_type %q, want %q", eventType, evt.Type)
		}
		if payload["foo"] != "bar" {
			t.Errorf("got payload %v, want %v", payload, evt.Payload)
		}
	})

	t.Run("NullPayload", func(t *testing.T) {
		// This test case checks that the server handles a `null` JSON payload
		// gracefully. Previously, this would cause a panic, because the JSON
		// decoder would decode `null` into a `nil` `*Event` pointer, and the
		// generic `web.HandleJSON` handler would then try to call the `Validate`
		// method on this `nil` pointer, leading to a panic.
		//
		// The fix was to wrap the `*Event` in a struct that does not have a
		// `Validate` method, and then manually validate the event inside the
		// handler, after checking for `nil`.
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("null"))
		res := httptest.NewRecorder()

		c.ServeHTTP(res, req)

		if res.Code != http.StatusBadRequest {
			t.Errorf("got status %d, want %d", res.Code, http.StatusBadRequest)
		}
	})
}

func TestEvent_Validate(t *testing.T) {
	testCases := []struct {
		name  string
		evt   *Event
		valid bool
	}{
		{
			name: "valid",
			evt: &Event{
				SessionID:  "test-session",
				AppName:    "test-app",
				AppVersion: "1.0.0",
				OS:         "linux",
				Type:       "test-event",
				Payload:    map[string]any{"foo": "bar"},
			},
			valid: true,
		},
		{
			name: "missing session_id",
			evt: &Event{
				AppName:    "test-app",
				AppVersion: "1.0.0",
				OS:         "linux",
				Type:       "test-event",
				Payload:    map[string]any{"foo": "bar"},
			},
			valid: false,
		},
		{
			name: "missing app_name",
			evt: &Event{
				SessionID:  "test-session",
				AppVersion: "1.0.0",
				OS:         "linux",
				Type:       "test-event",
				Payload:    map[string]any{"foo": "bar"},
			},
			valid: false,
		},
		{
			name: "missing app_version",
			evt: &Event{
				SessionID: "test-session",
				AppName:   "test-app",
				OS:        "linux",
				Type:      "test-event",
				Payload:   map[string]any{"foo": "bar"},
			},
			valid: false,
		},
		{
			name: "missing os",
			evt: &Event{
				SessionID:  "test-session",
				AppName:    "test-app",
				AppVersion: "1.0.0",
				Type:       "test-event",
				Payload:    map[string]any{"foo": "bar"},
			},
			valid: false,
		},
		{
			name: "missing event_type",
			evt: &Event{
				SessionID:  "test-session",
				AppName:    "test-app",
				AppVersion: "1.0.0",
				OS:         "linux",
				Payload:    map[string]any{"foo": "bar"},
			},
			valid: false,
		},
		{
			name: "missing payload",
			evt: &Event{
				SessionID:  "test-session",
				AppName:    "test-app",
				AppVersion: "1.0.0",
				OS:         "linux",
				Type:       "test-event",
			},
			valid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.evt.Validate()
			if tc.valid && err != nil {
				t.Errorf("got error %v, want nil", err)
			}
			if !tc.valid && err == nil {
				t.Error("got nil, want error")
			}
		})
	}
}

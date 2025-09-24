// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package apptelemetry

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.astrophena.name/base/testutil"

	_ "github.com/tailscale/sqlite"
)

func TestSQLiteCollector(t *testing.T) {
	t.Parallel()

	c, err := NewCollector(t.Context(), "file:/apptelemetry-test?vfs=memdb", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.db.ExecContext(t.Context(), "DELETE FROM app_telemetry_events"); err != nil {
		t.Fatal(err)
	}

	testCollector(t, c)
}

func testCollector(t *testing.T, c *Collector) {
	t.Helper()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

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
			payload                                       []byte
		)
		if err := c.db.QueryRowContext(context.Background(), `
			SELECT session_id, app_name, app_version, os, event_type, payload
			FROM app_telemetry_events
			WHERE session_id = ?
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

		var gotPayload map[string]any
		if err := json.Unmarshal(payload, &gotPayload); err != nil {
			t.Fatal(err)
		}
		if gotPayload["foo"] != "bar" {
			t.Errorf("got payload %v, want %v", gotPayload, evt.Payload)
		}
	})

	t.Run("NullPayload", func(t *testing.T) {
		t.Parallel()

		// This test case checks that the server handles a null JSON payload
		// gracefully. Previously, this would cause a panic.
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("null"))
		res := httptest.NewRecorder()

		c.ServeHTTP(res, req)

		if res.Code != http.StatusBadRequest {
			t.Errorf("got status %d, want %d", res.Code, http.StatusBadRequest)
		}
	})
}

func TestEvent_Validate(t *testing.T) {
	t.Parallel()

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

func TestExportHandler(t *testing.T) {
	t.Parallel()

	c, err := NewCollector(t.Context(), "file:/apptelemetry-export-test?vfs=memdb", time.Minute)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	// Seed the database with test data.
	events := []Event{
		{ // This will be the second row in the CSV
			SessionID:  "session-abc",
			AppName:    "app-1",
			AppVersion: "1.0.0",
			OS:         "linux",
			Type:       "app_start",
			Payload:    map[string]any{"user": "alpha"},
		},
		{ // This will be the first row in the CSV
			SessionID:  "session-def",
			AppName:    "app-2",
			AppVersion: "2.1.0",
			OS:         "macos",
			Type:       "feature_used",
			Payload:    map[string]any{"feature": "export"},
		},
	}

	for _, evt := range events {
		payload, err := json.Marshal(evt.Payload)
		if err != nil {
			t.Fatal(err)
		}
		// Insert with a small delay to ensure distinct created_at timestamps for reliable ordering.
		_, err = c.db.ExecContext(context.Background(), `
			INSERT INTO app_telemetry_events (session_id, app_name, app_version, os, event_type, payload, created_at)
			VALUES (?, ?, ?, ?, ?, ?, datetime('now'));
		`, evt.SessionID, evt.AppName, evt.AppVersion, evt.OS, evt.Type, payload)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	req := httptest.NewRequest(http.MethodGet, "/export", nil)
	rr := httptest.NewRecorder()
	c.ExportHandler().ServeHTTP(rr, req)

	testutil.AssertEqual(t, rr.Code, http.StatusOK)
	testutil.AssertEqual(t, rr.Header().Get("Content-Type"), "text/csv")
	testutil.AssertEqual(t, rr.Header().Get("Content-Disposition"), `attachment; filename="apptelemetry_export.csv"`)

	csvReader := csv.NewReader(rr.Body)
	records, err := csvReader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read csv response: %v", err)
	}

	if len(records) != 3 { // 1 header + 2 data rows.
		t.Fatalf("expected 3 records (1 header, 2 data), got %d", len(records))
	}

	// Verify header.
	expectedHeader := []string{"id", "session_id", "app_name", "app_version", "os", "event_type", "payload", "created_at"}
	testutil.AssertEqual(t, records[0], expectedHeader)
}

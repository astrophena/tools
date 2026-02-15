// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/tools/cmd/tgfeed/internal/state"
	"go.astrophena.name/tools/internal/filelock"
)

const testDefaultErrorTemplate = "Default error template."

type statsView struct {
	StartTime time.Time `json:"start_time"`
}

func TestAdmin(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, fs fs.FS) Config {
		t.Helper()
		stateDir := t.TempDir()

		if fs != nil {
			if err := os.CopyFS(stateDir, fs); err != nil {
				t.Fatalf("initializing state directory: %v", err)
			}
		}

		return Config{
			StateDir: stateDir,
			Store: state.NewStore(state.Options{
				StateDir:             stateDir,
				DefaultErrorTemplate: testDefaultErrorTemplate,
			}),
			ValidateConfig: func(context.Context, string) error {
				return errors.New("broken config")
			},
			IsRunLocked: func() bool {
				return filelock.IsLocked(filepath.Join(stateDir, ".run.lock"))
			},
		}
	}

	runTest := func(t *testing.T, cfg Config, r *http.Request, wantCode int, wantBody string) {
		t.Helper()
		h, err := Handler(cfg)
		if err != nil {
			t.Fatalf("creating handler: %v", err)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		testutil.AssertEqual(t, w.Code, wantCode)
		if wantBody != "" {
			body := w.Body.String()
			if !strings.Contains(body, wantBody) {
				t.Errorf("response body = %q, want to contain %q", body, wantBody)
			}
		}
	}

	initialFS := fstest.MapFS{
		"config.star":  {Data: []byte(`feed(url="https://example.com")`)},
		"state.json":   {Data: []byte(`{}`)},
		"error.tmpl":   {Data: []byte(`Custom error: %v`)},
		"stats/a.json": {Data: []byte(`{"start_time":"2023-01-01T12:00:00Z"}`)},
		"stats/b.json": {Data: []byte(`{"start_time":"2023-01-02T12:00:00Z"}`)},
	}

	t.Run("get config", func(t *testing.T) {
		cfg := setup(t, initialFS)
		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		runTest(t, cfg, req, http.StatusOK, `feed(url="https://example.com")`)
	})
	t.Run("put config (success)", func(t *testing.T) {
		cfg := setup(t, nil)
		cfg.ValidateConfig = func(context.Context, string) error { return nil }
		body := `feed(url="https://new.example.com")`
		req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
		runTest(t, cfg, req, http.StatusNoContent, "")
		content, _ := os.ReadFile(filepath.Join(cfg.StateDir, "config.star"))
		testutil.AssertEqual(t, string(content), body)
	})
	t.Run("put config (invalid)", func(t *testing.T) {
		cfg := setup(t, nil)
		req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`invalid starlark`))
		runTest(t, cfg, req, http.StatusBadRequest, "invalid config")
	})
	t.Run("put config (locked)", func(t *testing.T) {
		cfg := setup(t, initialFS)
		lockFile, err := filelock.Acquire(filepath.Join(cfg.StateDir, ".run.lock"), "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := lockFile.Release(); err != nil {
				t.Fatal(err)
			}
		})

		req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`feed()`))
		runTest(t, cfg, req, http.StatusConflict, "run is in progress")
	})

	t.Run("get state", func(t *testing.T) {
		cfg := setup(t, initialFS)
		req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
		runTest(t, cfg, req, http.StatusOK, `{}`)
	})
	t.Run("put state (success)", func(t *testing.T) {
		cfg := setup(t, nil)
		body := `{"https://new.example.com":{}}`
		req := httptest.NewRequest(http.MethodPut, "/api/state", strings.NewReader(body))
		runTest(t, cfg, req, http.StatusNoContent, "")
		content, _ := os.ReadFile(filepath.Join(cfg.StateDir, "state.json"))
		testutil.AssertEqual(t, string(content), body)
	})
	t.Run("put state (invalid JSON)", func(t *testing.T) {
		cfg := setup(t, nil)
		req := httptest.NewRequest(http.MethodPut, "/api/state", strings.NewReader(`{invalid`))
		runTest(t, cfg, req, http.StatusBadRequest, "invalid JSON")
	})

	t.Run("get error template", func(t *testing.T) {
		cfg := setup(t, initialFS)
		req := httptest.NewRequest(http.MethodGet, "/api/error-template", nil)
		runTest(t, cfg, req, http.StatusOK, "Custom error: %v")
	})
	t.Run("get error template (default)", func(t *testing.T) {
		cfg := setup(t, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/error-template", nil)
		runTest(t, cfg, req, http.StatusOK, testDefaultErrorTemplate)
	})
	t.Run("put error template", func(t *testing.T) {
		cfg := setup(t, nil)
		body := "New template"
		req := httptest.NewRequest(http.MethodPut, "/api/error-template", strings.NewReader(body))
		runTest(t, cfg, req, http.StatusNoContent, "")
		content, _ := os.ReadFile(filepath.Join(cfg.StateDir, "error.tmpl"))
		testutil.AssertEqual(t, string(content), body)
	})

	t.Run("get stats JSON", func(t *testing.T) {
		cfg := setup(t, initialFS)
		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		h, err := Handler(cfg)
		if err != nil {
			t.Fatalf("creating handler: %v", err)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		testutil.AssertEqual(t, w.Code, http.StatusOK)
		testutil.AssertEqual(t, w.Header().Get("Content-Type"), "application/json; charset=utf-8")

		var got []statsView
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}

		if len(got) != 2 {
			t.Fatalf("expected 2 stats entries, got %d", len(got))
		}
		// Check sorting (newest first).
		testutil.AssertEqual(t, got[0].StartTime.Format("2006-01-02T15:04:05Z07:00"), "2023-01-02T12:00:00Z")
		testutil.AssertEqual(t, got[1].StartTime.Format("2006-01-02T15:04:05Z07:00"), "2023-01-01T12:00:00Z")
	})
	t.Run("get stats JSON (no stats)", func(t *testing.T) {
		cfg := setup(t, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		runTest(t, cfg, req, http.StatusNotFound, "No stats available")
	})
}

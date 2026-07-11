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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/tools/cmd/tgfeed/internal/state"
	"go.astrophena.name/tools/cmd/tgfeed/internal/stats"
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
		statsStore := stats.OpenMemory(t.Name())
		t.Cleanup(func() {
			if err := statsStore.Close(); err != nil {
				t.Fatalf("closing stats store: %v", err)
			}
		})
		if err := statsStore.Bootstrap(t.Context()); err != nil {
			t.Fatalf("bootstrapping stats store: %v", err)
		}
		if fs != nil {
			if err := statsStore.SaveRun(t.Context(), &stats.Run{
				StartTime: time.Date(2023, time.January, 1, 12, 0, 0, 0, time.UTC),
			}); err != nil {
				t.Fatalf("saving first stats run: %v", err)
			}
			if err := statsStore.SaveRun(t.Context(), &stats.Run{
				StartTime: time.Date(2023, time.January, 2, 12, 0, 0, 0, time.UTC),
			}); err != nil {
				t.Fatalf("saving second stats run: %v", err)
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
			StatsStore: statsStore,
			StaticHashName: func(context.Context, string) string {
				return "static-test"
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
		"config.star": {Data: []byte(`feed(url="https://example.com")`)},
		"state.json":  {Data: []byte(`{}`)},
		"error.tmpl":  {Data: []byte(`Custom error: %v`)},
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

	t.Run("stats page", func(t *testing.T) {
		cfg := setup(t, initialFS)
		req := httptest.NewRequest(http.MethodGet, "/stats", nil)
		runTest(t, cfg, req, http.StatusOK, "This chart can't be rendered.")
	})
	t.Run("stats fragment", func(t *testing.T) {
		cfg := setup(t, initialFS)
		req := httptest.NewRequest(http.MethodGet, "/stats?auto_refresh=true", nil)
		req.Header.Set("HX-Target", "stats-content")
		h, err := Handler(cfg)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		testutil.AssertEqual(t, w.Code, http.StatusOK)
		body := w.Body.String()
		if !strings.Contains(body, `id="stats-content"`) || !strings.Contains(body, `hx-trigger="every 30s"`) {
			t.Fatalf("unexpected stats fragment: %s", body)
		}
		if strings.Contains(body, "<!DOCTYPE html>") {
			t.Fatalf("fragment contains page layout: %s", body)
		}
	})
	t.Run("stats invalid query", func(t *testing.T) {
		cfg := setup(t, initialFS)
		req := httptest.NewRequest(http.MethodGet, "/stats?auto_refresh=invalid", nil)
		runTest(t, cfg, req, http.StatusOK, `invalid &#34;auto_refresh&#34; query parameter`)
	})
	t.Run("stats missing selected run", func(t *testing.T) {
		cfg := setup(t, initialFS)
		req := httptest.NewRequest(http.MethodGet, "/stats?started_at_unix=1", nil)
		runTest(t, cfg, req, http.StatusOK, "selected run 1 is no longer available")
	})
	t.Run("reject unknown fragment", func(t *testing.T) {
		cfg := setup(t, initialFS)
		req := httptest.NewRequest(http.MethodGet, "/stats", nil)
		req.Header.Set("HX-Target", "unknown")
		runTest(t, cfg, req, http.StatusBadRequest, "Your request is invalid")
	})
	t.Run("configuration page", func(t *testing.T) {
		cfg := setup(t, initialFS)
		req := httptest.NewRequest(http.MethodGet, "/config", nil)
		runTest(t, cfg, req, http.StatusOK, `data-code-editor`)
	})
	t.Run("configuration page has native controls", func(t *testing.T) {
		cfg := setup(t, initialFS)
		req := httptest.NewRequest(http.MethodGet, "/config", nil)
		h, err := Handler(cfg)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		body := w.Body.String()
		wantControls := []string{
			`href="/stats"`,
			`href="/config"`,
			`formaction="/config/config"`,
			`formmethod="post"`,
		}
		for _, want := range wantControls {
			if !strings.Contains(body, want) {
				t.Errorf("response body does not contain %q", want)
			}
		}
	})
	t.Run("configuration page escapes editor content", func(t *testing.T) {
		cfg := setup(t, fstest.MapFS{
			"config.star": {
				Data: []byte(`</textarea><script>alert("x")</script>`),
			},
		})
		req := httptest.NewRequest(http.MethodGet, "/config", nil)
		h, err := Handler(cfg)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		body := w.Body.String()
		if strings.Contains(body, `<script>alert("x")</script>`) {
			t.Fatal("editor content was rendered as executable markup")
		}
		if !strings.Contains(body, `&lt;/textarea&gt;`) {
			t.Fatalf("escaped editor content is missing: %s", body)
		}
	})
	t.Run("save config form", func(t *testing.T) {
		cfg := setup(t, nil)
		cfg.ValidateConfig = func(context.Context, string) error { return nil }
		body := "config=" + url.QueryEscape(`feed(url="https://form.example.com")`)
		req := httptest.NewRequest(http.MethodPost, "/config/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Target", "config-panel")
		runTest(t, cfg, req, http.StatusOK, "Synced")
	})
	t.Run("save invalid config form", func(t *testing.T) {
		cfg := setup(t, nil)
		body := "config=" + url.QueryEscape("broken")
		req := httptest.NewRequest(http.MethodPost, "/config/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Target", "config-panel")
		runTest(t, cfg, req, http.StatusOK, "invalid config")
	})
	t.Run("save locked config form", func(t *testing.T) {
		cfg := setup(t, initialFS)
		lockFile, err := filelock.Acquire(filepath.Join(cfg.StateDir, ".run.lock"), "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := lockFile.Release(); err != nil {
				t.Error(err)
			}
		})
		body := "config=" + url.QueryEscape(`feed(url="https://form.example.com")`)
		req := httptest.NewRequest(http.MethodPost, "/config/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Target", "config-panel")
		runTest(t, cfg, req, http.StatusOK, "run is in progress")
	})
	t.Run("save all reports partial failure", func(t *testing.T) {
		cfg := setup(t, initialFS)
		body := url.Values{
			"config":         {"broken"},
			"error_template": {"Updated template"},
		}.Encode()
		req := httptest.NewRequest(http.MethodPost, "/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Target", "dashboard-content")
		runTest(t, cfg, req, http.StatusOK, "Some changes failed to save")
		content, err := os.ReadFile(filepath.Join(cfg.StateDir, "error.tmpl"))
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, string(content), "Updated template")
	})
}

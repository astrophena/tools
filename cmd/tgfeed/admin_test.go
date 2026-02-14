// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"encoding/csv"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/tools/internal/filelock"
)

func TestAdmin(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, fs fs.FS) *fetcher {
		t.Helper()
		f := &fetcher{
			stateDir: t.TempDir(),
			logf:     t.Logf,
		}
		f.init.Do(func() { f.doInit(t.Context()) })

		if fs != nil {
			if err := os.CopyFS(f.stateDir, fs); err != nil {
				t.Fatalf("initializing state directory: %v", err)
			}
		}
		return f
	}

	runTest := func(t *testing.T, f *fetcher, r *http.Request, wantCode int, wantBody string) {
		t.Helper()
		w := httptest.NewRecorder()
		mux := http.NewServeMux()
		// Re-register handlers to ensure they use the test's fetcher instance.
		mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				f.handleGetConfig(w, r)
			case http.MethodPut:
				f.handlePutConfig(w, r)
			}
		})
		mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				f.handleGetState(w, r)
			case http.MethodPut:
				f.handlePutState(w, r)
			}
		})
		mux.HandleFunc("/api/error-template", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				f.handleGetErrorTemplate(w, r)
			case http.MethodPut:
				f.handlePutErrorTemplate(w, r)
			}
		})
		mux.HandleFunc("/debug/stats.csv", f.handleStatsCSV)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				http.Redirect(w, r, "/debug/", http.StatusFound)
				return
			}
			http.NotFound(w, r)
		})

		mux.ServeHTTP(w, r)

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
		f := setup(t, initialFS)
		req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		runTest(t, f, req, http.StatusOK, `feed(url="https://example.com")`)
	})
	t.Run("put config (success)", func(t *testing.T) {
		f := setup(t, nil) // Start with empty state dir
		body := `feed(url="https://new.example.com")`
		req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
		runTest(t, f, req, http.StatusNoContent, "")
		content, _ := os.ReadFile(filepath.Join(f.stateDir, "config.star"))
		testutil.AssertEqual(t, string(content), body)
	})
	t.Run("put config (invalid)", func(t *testing.T) {
		f := setup(t, nil)
		req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`invalid starlark`))
		runTest(t, f, req, http.StatusBadRequest, "invalid config")
	})
	t.Run("put config (locked)", func(t *testing.T) {
		f := setup(t, initialFS)
		lockFile, err := filelock.Acquire(filepath.Join(f.stateDir, ".run.lock"), "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := lockFile.Release(); err != nil {
				t.Fatal(err)
			}
		})

		req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`feed()`))
		runTest(t, f, req, http.StatusConflict, "run is in progress")
	})

	t.Run("get state", func(t *testing.T) {
		f := setup(t, initialFS)
		req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
		runTest(t, f, req, http.StatusOK, `{}`)
	})
	t.Run("put state (success)", func(t *testing.T) {
		f := setup(t, nil)
		body := `{"https://new.example.com":{}}`
		req := httptest.NewRequest(http.MethodPut, "/api/state", strings.NewReader(body))
		runTest(t, f, req, http.StatusNoContent, "")
		content, _ := os.ReadFile(filepath.Join(f.stateDir, "state.json"))
		testutil.AssertEqual(t, string(content), body)
	})
	t.Run("put state (invalid JSON)", func(t *testing.T) {
		f := setup(t, nil)
		req := httptest.NewRequest(http.MethodPut, "/api/state", strings.NewReader(`{invalid`))
		runTest(t, f, req, http.StatusBadRequest, "invalid JSON")
	})

	t.Run("get error template", func(t *testing.T) {
		f := setup(t, initialFS)
		req := httptest.NewRequest(http.MethodGet, "/api/error-template", nil)
		runTest(t, f, req, http.StatusOK, "Custom error: %v")
	})
	t.Run("get error template (default)", func(t *testing.T) {
		f := setup(t, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/error-template", nil)
		runTest(t, f, req, http.StatusOK, defaultErrorTemplate)
	})
	t.Run("put error template", func(t *testing.T) {
		f := setup(t, nil)
		body := "New template"
		req := httptest.NewRequest(http.MethodPut, "/api/error-template", strings.NewReader(body))
		runTest(t, f, req, http.StatusNoContent, "")
		content, _ := os.ReadFile(filepath.Join(f.stateDir, "error.tmpl"))
		testutil.AssertEqual(t, string(content), body)
	})

	t.Run("get stats CSV", func(t *testing.T) {
		f := setup(t, initialFS)
		req := httptest.NewRequest(http.MethodGet, "/debug/stats.csv", nil)
		w := httptest.NewRecorder()
		f.handleStatsCSV(w, req)

		testutil.AssertEqual(t, w.Code, http.StatusOK)
		testutil.AssertEqual(t, w.Header().Get("Content-Type"), "text/csv")

		r := csv.NewReader(w.Body)
		records, err := r.ReadAll()
		if err != nil {
			t.Fatal(err)
		}

		if len(records) != 3 {
			t.Fatalf("expected 3 records (header + 2 data), got %d", len(records))
		}
		// Check header.
		testutil.AssertEqual(t, records[0][0], "StartTime")
		// Check sorting (newest first).
		testutil.AssertEqual(t, records[1][0], "2023-01-02T12:00:00Z")
		testutil.AssertEqual(t, records[2][0], "2023-01-01T12:00:00Z")
	})
	t.Run("get stats CSV (no stats)", func(t *testing.T) {
		f := setup(t, nil)
		req := httptest.NewRequest(http.MethodGet, "/debug/stats.csv", nil)
		runTest(t, f, req, http.StatusNotFound, "No stats available")
	})
}

// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/web"
)

func TestRoutes(t *testing.T) {
	t.Parallel()

	e := testEngine(t, testMux(t, nil))

	for _, path := range []string{
		"/debug/",
		"/debug/logs",
		"/debug/reload",
		"/" + e.srv.StaticHashName("static/css/main.css"),
		"/" + e.srv.StaticHashName("static/js/logs.js"),
	} {
		_, err := request.Make[request.IgnoreResponse](t.Context(), request.Params{
			Method:     http.MethodGet,
			URL:        path,
			HTTPClient: testutil.MockHTTPClient(e.mux),
		})
		if err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func TestHealth(t *testing.T) {
	t.Parallel()

	e := testEngine(t, testMux(t, nil))
	health, err := request.Make[web.HealthResponse](t.Context(), request.Params{
		Method:     http.MethodGet,
		URL:        "/health",
		HTTPClient: testutil.MockHTTPClient(e.mux),
	})
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, health.OK, true)
}

type mockTgAuth struct {
	loggedIn bool
}

func (m *mockTgAuth) LoggedIn(r *http.Request) bool {
	return m.loggedIn
}

func (m *mockTgAuth) LoginHandler(redirectTarget string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
}

func (m *mockTgAuth) LogoutHandler(redirectTarget string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
}

func (m *mockTgAuth) Middleware(enforceAuth bool) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
}

func TestHandleRoot(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		dev      bool
		loggedIn bool
		path     string
		wantCode int
		wantURL  string
	}{
		"not found": {
			path:     "/not-found",
			wantCode: http.StatusNotFound,
		},
		"dev": {
			dev:      true,
			path:     "/",
			wantCode: http.StatusFound,
			wantURL:  "/debug/bot",
		},
		"logged in": {
			loggedIn: true,
			path:     "/",
			wantCode: http.StatusFound,
			wantURL:  "/debug/",
		},
		"not logged in": {
			path:     "/",
			wantCode: http.StatusFound,
			wantURL:  "https://go.astrophena.name/tools/cmd/starlet",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := testEngine(t, testMux(t, nil))
			e.dev = tc.dev
			e.tgAuth = &mockTgAuth{loggedIn: tc.loggedIn}

			r := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			e.handleRoot(w, r)

			testutil.AssertEqual(t, w.Code, tc.wantCode)
			if tc.wantURL != "" {
				testutil.AssertEqual(t, w.Header().Get("Location"), tc.wantURL)
			}
		})
	}
}

func TestHandleGeminiProxyToken(t *testing.T) {
	t.Parallel()

	e := testEngine(t, testMux(t, nil))
	e.geminiProxySecretKey = "test"

	t.Run("get", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/debug/gemini-token", nil)
		w := httptest.NewRecorder()
		e.handleGeminiProxyToken(w, r)
		testutil.AssertEqual(t, w.Code, http.StatusOK)
	})

	t.Run("post", func(t *testing.T) {
		form := url.Values{}
		form.Add("duration", "1h")
		form.Add("rate_limit", "10")
		form.Add("description", "test token")

		r := httptest.NewRequest(http.MethodPost, "/debug/gemini-token", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		e.handleGeminiProxyToken(w, r)
		testutil.AssertEqual(t, w.Code, http.StatusOK)
	})

	t.Run("post invalid duration", func(t *testing.T) {
		form := url.Values{}
		form.Add("duration", "invalid")
		r := httptest.NewRequest(http.MethodPost, "/debug/gemini-token", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		e.handleGeminiProxyToken(w, r)
		testutil.AssertEqual(t, w.Code, http.StatusBadRequest)
	})
}

func TestTgInterceptor(t *testing.T) {
	t.Parallel()

	interceptor := newTgInterceptor(slog.New(slog.NewTextHandler(io.Discard, nil)), http.DefaultTransport)

	t.Run("intercept", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "https://api.telegram.org/bot123/sendMessage", strings.NewReader(`{"chat_id":123,"text":"hello"}`))
		_, err := interceptor.RoundTrip(r)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("no intercept", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
		_, err := interceptor.RoundTrip(r)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestOnRenderRedirect(t *testing.T) {
	e := testEngineWithoutRoutes(t, testMux(t, nil))
	e.onRender = true
	e.host = "bot.example.com"
	t.Setenv("RENDER_EXTERNAL_HOSTNAME", "starlet.onrender.com")
	if err := e.init.Get(func() error {
		return e.doInit(t.Context())
	}); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodGet, "https://starlet.onrender.com/", nil)
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)

	testutil.AssertEqual(t, w.Code, http.StatusMovedPermanently)
	testutil.AssertEqual(t, w.Header().Get("Location"), "https://bot.example.com/")
}

func TestEngine_debugAuth(t *testing.T) {
	t.Parallel()

	e := testEngine(t, testMux(t, nil))

	cases := map[string]struct {
		url        string
		dev        bool
		wantStatus int
		wantNext   bool
	}{
		"not /debug, not dev": {
			url:      "/",
			wantNext: true,
		},
		"not /debug, dev": {
			dev:      true,
			url:      "/",
			wantNext: true,
		},
		"/debug, not logged in, not dev": {
			url:        "/debug/",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e.dev = tc.dev

			var nextCalled bool
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
			})

			r := httptest.NewRequest(http.MethodGet, tc.url, nil)
			w := httptest.NewRecorder()

			e.debugAuth(next).ServeHTTP(w, r)

			if tc.wantStatus != 0 {
				testutil.AssertEqual(t, w.Code, tc.wantStatus)
			}
			testutil.AssertEqual(t, nextCalled, tc.wantNext)
		})
	}
}

func TestReload(t *testing.T) {
	t.Parallel()

	tm := testMux(t, nil)
	tm.gist = txtarToGist(t, []byte("-- bot.star --\nprint(\"reloaded\")\n"))

	cases := map[string]struct {
		authHeader string
		dev        bool
		wantStatus int
		wantBody   string
	}{
		"unauthorized": {
			wantStatus: http.StatusUnauthorized,
			wantBody:   `{"status":"error","error":"unauthorized"}`,
		},
		"authorized": {
			authHeader: "Bearer " + testEngine(t, tm).reloadToken,
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"success"}`,
		},
		"dev mode": {
			authHeader: "Bearer " + testEngine(t, tm).reloadToken,
			dev:        true,
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"success"}`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := testEngine(t, tm)

			if tc.dev {
				e.dev = true
				tmp := t.TempDir()
				e.botStatePath = tmp
				if err := os.WriteFile(filepath.Join(tmp, "bot.star"), []byte{}, 0o644); err != nil {
					t.Fatal(err)
				}
			}

			r := httptest.NewRequest(http.MethodPost, "/reload", nil)
			r.Header.Set("Authorization", tc.authHeader)
			w := httptest.NewRecorder()

			e.handleReload(w, r)

			var got bytes.Buffer
			if err := json.Compact(&got, w.Body.Bytes()); err != nil {
				t.Fatal(err)
			}

			testutil.AssertEqual(t, w.Code, tc.wantStatus)
			testutil.AssertEqual(t, got.String(), tc.wantBody)
		})
	}
}

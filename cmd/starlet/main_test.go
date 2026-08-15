// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/request"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/web"
	"go.astrophena.name/base/web/service"
)

func TestEngineEndpoints(t *testing.T) {
	const (
		gistID   = "test-gist"
		ghToken  = "github-secret"
		host     = "bot.example.com"
		tgSecret = "telegram-secret"
		tgToken  = "telegram-token"
	)

	var webhook struct {
		URL         string `json:"url"`
		SecretToken string `json:"secret_token"`
	}
	httpc := testutil.MockHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Host == "api.telegram.org" && r.URL.Path == "/bot"+tgToken+"/getMe":
			fmt.Fprint(w, `{"ok":true,"result":{"id":987654321,"username":"testbot"}}`)
		case r.Method == http.MethodGet && r.URL.Host == "api.github.com" && r.URL.Path == "/gists/"+gistID:
			if got := r.Header.Get("Authorization"); got != "Bearer "+ghToken {
				t.Errorf("Authorization = %q, want bearer token", got)
			}
			fmt.Fprint(w, `{"files":{"bot.star":{"content":"def handle(update):\n    pass\n"}}}`)
		case r.Method == http.MethodPost && r.URL.Host == "api.telegram.org" && r.URL.Path == "/bot"+tgToken+"/setWebhook":
			if err := json.NewDecoder(r.Body).Decode(&webhook); err != nil {
				t.Fatal(err)
			}
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))

	level := new(slog.LevelVar)
	ctx := logger.Put(t.Context(), &logger.Logger{
		Logger: slog.New(slog.NewTextHandler(t.Output(), nil)),
		Level:  level,
	})
	e := &engine{
		ghToken:      ghToken,
		gistID:       gistID,
		host:         host,
		httpc:        httpc,
		llmUsagePath: filepath.Join(t.TempDir(), "llm-usage.json"),
		tgOwner:      123456789,
		tgSecret:     tgSecret,
		tgToken:      tgToken,
	}

	public, err := e.PublicEndpoint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := e.AdminEndpoint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := e.Shutdown(t.Context()); err != nil {
			t.Fatal(err)
		}
	})

	if public.Mux == admin.Mux {
		t.Fatal("public and admin endpoints share a mux")
	}
	if webhook.URL != "https://"+host+"/telegram" || webhook.SecretToken != tgSecret {
		t.Fatalf("setWebhook payload = %+v", webhook)
	}

	publicClient := startTestServer(t, ctx, public)
	assertResponse(t, ctx, publicClient, http.MethodGet, "/", "", http.StatusFound, "https://go.astrophena.name/tools/cmd/starlet")
	assertResponse(t, ctx, publicClient, http.MethodGet, "/env", "", http.StatusOK, "Starlark Environment")
	assertResponse(t, ctx, publicClient, http.MethodPost, "/telegram", tgSecret, http.StatusOK, `"status": "ok"`)

	adminClient := startTestServer(t, ctx, admin)
	assertResponse(t, ctx, adminClient, http.MethodGet, "/", "", http.StatusFound, "/debug/")
}

func startTestServer(t *testing.T, ctx context.Context, endpoint *service.EndpointConfig) *http.Client {
	t.Helper()

	socket := filepath.Join(t.TempDir(), "server.sock")
	ready := make(chan struct{})
	server := &web.Server{
		Addr:     socket,
		Mux:      endpoint.Mux,
		StaticFS: endpoint.StaticFS,
		CSP:      endpoint.CSP,
		Ready:    func() { close(ready) },
	}
	serverCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- server.ListenAndServe(serverCtx)
	}()
	select {
	case <-ready:
	case err := <-done:
		cancel()
		t.Fatalf("starting test server: %v", err)
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return new(net.Dialer).DialContext(ctx, "unix", socket)
		},
	}
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		cancel()
		if err := <-done; err != nil {
			t.Errorf("stopping test server: %v", err)
		}
	})
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func assertResponse(t *testing.T, ctx context.Context, client *http.Client, method, path, secret string, wantCode int, want string) {
	t.Helper()
	var body any
	if method == http.MethodPost {
		body = map[string]any{}
	}
	var headers map[string]string
	if secret != "" {
		headers = map[string]string{"X-Telegram-Bot-Api-Secret-Token": secret}
	}
	got, err := request.Make[request.Bytes](ctx, request.Params{
		Method:         method,
		URL:            "http://example.com" + path,
		Headers:        headers,
		Body:           body,
		WantStatusCode: wantCode,
		HTTPClient:     client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), want) {
		t.Fatalf("%s %s response does not contain %q:\n%s", method, path, want, got)
	}
}

// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/cli/clitest"
	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/request"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.astrophena.name/tools/internal/api/github/gist"
	"go.astrophena.name/tools/internal/web"
)

// Typical Telegram Bot API token, copied from docs.
const tgToken = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

var update = flag.Bool("update", false, "update golden files in testdata")

func TestEngineMain(t *testing.T) {
	t.Parallel()

	clitest.Run(t, func(t *testing.T) *engine {
		e := new(engine)
		e.httpc = testutil.MockHTTPClient(testMux(t, nil).mux)
		e.noServerStart = true
		return e
	}, map[string]clitest.Case[*engine]{
		"prints usage with help flag": {
			Args:    []string{"-h"},
			WantErr: flag.ErrHelp,
		},
		"overrides telegram token passed from flag by env": {
			Args: []string{"-tg-token", "blablabla"},
			Env: map[string]string{
				"TG_TOKEN": tgToken,
			},
			CheckFunc: func(t *testing.T, e *engine) {
				testutil.AssertEqual(t, e.tgToken, tgToken)
			},
		},
		"version": {
			Args:    []string{"-version"},
			WantErr: cli.ErrExitVersion,
		},
	},
	)
}

func TestListenAndServe(t *testing.T) {
	t.Parallel()

	e := testEngine(t, testMux(t, nil))

	// Find a free port for us.
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to find a free port: %v", err)
	}
	addr := fmt.Sprintf("localhost:%d", port)

	var wg sync.WaitGroup

	ready := make(chan struct{})
	e.ready = func() {
		ready <- struct{}{}
	}
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())

	var stdout, stderr bytes.Buffer

	wg.Add(1)
	go func() {
		defer wg.Done()
		env := &cli.Env{
			Args:   []string{"-addr", addr},
			Getenv: os.Getenv,
			Stdout: &stdout,
			Stderr: &stderr,
		}
		if err := cli.Run(cli.WithEnv(ctx, env), e); err != nil {
			errCh <- err
		}
	}()

	// Wait until the server is ready.
	select {
	case err := <-errCh:
		t.Fatalf("Test server crashed during startup or runtime: %v", err)
	case <-ready:
	}

	// Make some HTTP requests.
	urls := []struct {
		url        string
		wantStatus int
	}{
		{url: "/static/css/main.css", wantStatus: http.StatusOK},
		{url: "/static/" + web.StaticFS.HashName("css/main.css"), wantStatus: http.StatusOK},
		{url: "/health", wantStatus: http.StatusOK},
	}

	for _, u := range urls {
		req, err := http.Get("http://" + addr + u.url)
		if err != nil {
			t.Fatal(err)
		}
		if req.StatusCode != u.wantStatus {
			t.Fatalf("GET %s: want status code %d, got %d", u.url, u.wantStatus, req.StatusCode)
		}
	}

	// Try to gracefully shutdown the server.
	cancel()
	// Wait until the server shuts down.
	wg.Wait()
	// See if the server failed to shutdown.
	select {
	case err := <-errCh:
		t.Fatalf("Test server crashed during shutdown: %v", err)
	default:
	}
}

// getFreePort asks the kernel for a free open port that is ready to use.
// Copied from
// https://github.com/phayes/freeport/blob/74d24b5ae9f58fbe4057614465b11352f71cdbea/freeport.go.
func getFreePort() (port int, err error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func TestHealth(t *testing.T) {
	t.Parallel()

	e := testEngine(t, testMux(t, nil))
	health, err := request.Make[web.HealthResponse](context.Background(), request.Params{
		Method:     http.MethodGet,
		URL:        "/health",
		HTTPClient: testutil.MockHTTPClient(e.mux),
	})
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, health.OK, true)
}

func TestHandleTelegramWebhook(t *testing.T) {
	t.Parallel()

	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, match string) []byte {
		t.Parallel()

		ar, err := txtar.ParseFile(match)
		if err != nil {
			t.Fatal(err)
		}

		if len(ar.Files) != 2 ||
			ar.Files[0].Name != "bot.star" ||
			ar.Files[1].Name != "update.json" {
			t.Fatalf("%s txtar should contain only two files: bot.star and update.json", match)
		}

		var upd json.RawMessage
		for _, f := range ar.Files {
			if f.Name == "update.json" {
				upd = json.RawMessage(f.Data)
			}
		}

		tm := testMux(t, nil)
		tm.gist = txtarToGist(t, readFile(t, match))
		e := testEngine(t, tm)

		_, err = request.Make[any](context.Background(), request.Params{
			Method: http.MethodPost,
			URL:    "/telegram",
			Body:   upd,
			Headers: map[string]string{
				"X-Telegram-Bot-Api-Secret-Token": e.tgSecret,
			},
			HTTPClient: testutil.MockHTTPClient(e.mux),
		})
		if err != nil {
			t.Fatal(err)
		}

		calls, err := json.MarshalIndent(tm.telegramCalls, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return calls
	}, *update)
}

func TestSelfPing(t *testing.T) {
	recv := make(chan struct{})

	e := testEngine(t, testMux(t, map[string]http.HandlerFunc{
		"GET bot.astrophena.name/health": func(w http.ResponseWriter, r *http.Request) {
			testutil.AssertEqual(t, r.URL.Scheme, "https")
			web.RespondJSON(w, web.HealthResponse{OK: true})
			recv <- struct{}{}
		},
	}))

	interval := 10 * time.Millisecond
	getenv := func(key string) string {
		if key != "RENDER_EXTERNAL_URL" {
			t.Fatalf("selfPing tried to read environment variable %s", key)
		}
		return "https://bot.astrophena.name"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go e.selfPing(ctx, getenv, interval)

	<-recv
}

//go:embed testdata/message.txtar
var reloadTxtar []byte

func TestReload(t *testing.T) {
	t.Parallel()

	tm := testMux(t, nil)
	tm.gist = txtarToGist(t, reloadTxtar)
	e := testEngine(t, tm)

	cases := map[string]struct {
		authHeader string
		wantStatus int
		wantBody   string
	}{
		"unauthorized": {
			wantStatus: http.StatusUnauthorized,
			wantBody:   `{"status":"error","error":"unauthorized"}`,
		},
		"authorized": {
			authHeader: "Bearer " + e.reloadToken,
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"success"}`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

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

func TestSetWebhook(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		host        string
		wantSetHook bool
		wantErr     error
	}{
		"host not set": {
			wantErr: errNoHost,
		},
		"webhook set": {
			host:        "bot.astrophena.name",
			wantSetHook: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var called atomic.Bool

			tm := testMux(t, map[string]http.HandlerFunc{
				"POST api.telegram.org/{token}/setWebhook": func(w http.ResponseWriter, r *http.Request) {
					testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
					wantURL := "https://bot.astrophena.name/telegram"
					gotURL := testutil.UnmarshalJSON[map[string]any](t, read(t, r.Body))["url"]
					testutil.AssertEqual(t, gotURL, wantURL)

					w.WriteHeader(http.StatusOK)
					web.RespondJSON(w, map[string]bool{"ok": true})
					called.Store(true)
				},
			})
			e := testEngine(t, tm)
			e.host = tc.host

			err := e.setWebhook(context.Background())

			if tc.wantErr != nil {
				if err == nil || err.Error() != tc.wantErr.Error() {
					t.Fatalf("wanted error %v, got %v", tc.wantErr, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}

			if tc.wantSetHook {
				if !called.Load() {
					t.Fatalf("setWebhook must be called for this case")
				}
			}
		})
	}
}

func testEngine(t *testing.T, m *mux) *engine {
	t.Helper()
	e := &engine{
		ghToken:     "test",
		gistID:      "test",
		httpc:       testutil.MockHTTPClient(m.mux),
		tgOwner:     123456789,
		stderr:      logger.Logf(t.Logf),
		reloadToken: "foobar",
		tgSecret:    "test",
		tgToken:     tgToken,
	}
	if err := e.init.Get(e.doInit); err != nil {
		t.Fatal(err)
	}
	return e
}

type mux struct {
	mux           *http.ServeMux
	mu            sync.Mutex
	gist          []byte
	telegramCalls []call
}

type call struct {
	Method string         `json:"method"`
	Args   map[string]any `json:"args"`
}

const (
	getGist       = "GET api.github.com/gists/test"
	getMeTelegram = "GET api.telegram.org/{token}/getMe"
	postTelegram  = "POST api.telegram.org/{token}/{method}"
)

func testMux(t *testing.T, overrides map[string]http.HandlerFunc) *mux {
	m := &mux{mux: http.NewServeMux()}
	m.mux.HandleFunc(getGist, orHandler(overrides[getGist], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, r.Header.Get("Authorization"), "Bearer test")
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.gist != nil {
			w.Write(m.gist)
		}
	}))
	m.mux.HandleFunc(getMeTelegram, orHandler(overrides[getMeTelegram], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
		var resp getMeResponse
		resp.OK = true
		resp.Result.ID = 123456789
		resp.Result.Username = "foo_bot"
		web.RespondJSON(w, resp)
	}))
	m.mux.HandleFunc(postTelegram, orHandler(overrides[postTelegram], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
		m.mu.Lock()
		defer m.mu.Unlock()
		b := read(t, r.Body)
		m.telegramCalls = append(m.telegramCalls, call{
			Method: r.PathValue("method"),
			Args:   testutil.UnmarshalJSON[map[string]any](t, b),
		})
		jsonOK(w)
	}))
	for pat, h := range overrides {
		if pat == getGist || pat == postTelegram || pat == getMeTelegram {
			continue
		}
		m.mux.HandleFunc(pat, h)
	}
	return m
}

func orHandler(hh ...http.HandlerFunc) http.HandlerFunc {
	for _, h := range hh {
		if h != nil {
			return h
		}
	}
	return nil
}

func readFile(t *testing.T, path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func read(t *testing.T, r io.Reader) []byte {
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func txtarToGist(t *testing.T, b []byte) []byte {
	ar := txtar.Parse(b)

	g := &gist.Gist{
		Files: make(map[string]gist.File),
	}

	for _, f := range ar.Files {
		g.Files[f.Name] = gist.File{Content: string(f.Data)}
	}

	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	return b
}

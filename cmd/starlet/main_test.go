package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/logger"
	"go.astrophena.name/tools/internal/request"
	"go.astrophena.name/tools/internal/web"
)

// Typical Telegram Bot API token, copied from docs.
const tgToken = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

func TestEngineMain(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args               []string
		env                map[string]string
		wantErr            error
		wantNothingPrinted bool
		wantInStdout       string
		wantInStderr       string
		checkFunc          func(t *testing.T, e *engine)
	}{
		"prints usage with help flag": {
			args:         []string{"-h"},
			wantErr:      flag.ErrHelp,
			wantInStderr: "Usage: starlet",
		},
		"overrides telegram token passed from flag by env": {
			args: []string{"-tg-token", "blablabla"},
			env: map[string]string{
				"TG_TOKEN": "foobarfoo",
			},
			checkFunc: func(t *testing.T, e *engine) {
				testutil.AssertEqual(t, e.tgToken, "foobarfoo")
			},
		},
		"version": {
			args: []string{"-version"},
		},
	}

	getenvFunc := func(env map[string]string) func(string) string {
		return func(name string) string {
			if env == nil {
				return ""
			}
			return env[name]
		}
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var (
				e              = new(engine)
				stdout, stderr bytes.Buffer
			)
			e.noServerStart = true

			err := e.main(context.Background(), tc.args, getenvFunc(tc.env), &stdout, &stderr)

			// Don't use && because we want to trap all cases where err is
			// nil.
			if err == nil {
				if tc.wantErr != nil {
					t.Fatalf("must fail with error: %v", tc.wantErr)
				}
			}

			if err != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("got error: %v", err)
			}

			if tc.wantNothingPrinted {
				if stdout.String() != "" {
					t.Errorf("stdout must be empty, got: %q", stdout.String())
				}
				if stderr.String() != "" {
					t.Errorf("stderr must be empty, got: %q", stderr.String())
				}
			}

			if tc.wantInStdout != "" && !strings.Contains(stdout.String(), tc.wantInStdout) {
				t.Errorf("stdout must contain %q, got: %q", tc.wantInStdout, stdout.String())
			}
			if tc.wantInStderr != "" && !strings.Contains(stderr.String(), tc.wantInStderr) {
				t.Errorf("stderr must contain %q, got: %q", tc.wantInStderr, stderr.String())
			}

			if tc.checkFunc != nil {
				tc.checkFunc(t, e)
			}
		})
	}
}

func TestHealth(t *testing.T) {
	e := testEngine(t, testMux(t, nil))
	health, err := request.Make[web.HealthResponse](context.Background(), request.Params{
		Method:     http.MethodGet,
		URL:        "/health",
		HTTPClient: testutil.MockHTTPClient(t, e.mux),
	})
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, health.OK, true)
}

var update = flag.Bool("update", false, "update golden files in testdata")

func TestHandleTelegramWebhook(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, match string) []byte {
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
			HTTPClient: testutil.MockHTTPClient(t, e.mux),
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

func testEngine(t *testing.T, m *mux) *engine {
	e := &engine{
		ghToken:  "test",
		gistID:   "test",
		httpc:    testutil.MockHTTPClient(t, m.mux),
		tgOwner:  123456789,
		stderr:   logger.Logf(t.Logf),
		tgSecret: "test",
		tgToken:  tgToken,
	}
	e.init.Do(e.doInit)
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
	getGist      = "GET api.github.com/gists/test"
	postTelegram = "POST api.telegram.org/{token}/{method}"
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
		if pat == getGist || pat == postTelegram {
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

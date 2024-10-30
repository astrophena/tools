// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/api/google/serviceaccount"
	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/web"
)

// Typical Telegram Bot API token, copied from docs.
const tgToken = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

var (
	//go:embed testdata/gists/main.txtar
	gistTxtar []byte
)

// TODO: Test keep and block rules, edit mode.

var update = flag.Bool("update", false, "update golden files in testdata")

func TestFetcherMain(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args               []string
		env                map[string]string
		wantErr            error
		wantNothingPrinted bool
		wantInStdout       string
		wantInStderr       string
		checkFunc          func(t *testing.T, f *fetcher)
	}{
		"prints usage without flags": {
			args:         []string{},
			wantErr:      cli.ErrInvalidArgs,
			wantInStderr: "Usage: tgfeed",
		},
		"run": {
			args:               []string{"-run"},
			wantNothingPrinted: true,
		},
		"reenable disabled feed": {
			args:               []string{"-reenable", "https://example.com/disabled.xml"},
			wantNothingPrinted: true,
			checkFunc: func(t *testing.T, f *fetcher) {
				testutil.AssertEqual(t, f.state["https://example.com/disabled.xml"].Disabled, false)
			},
		},
		"reenable non-existent feed": {
			args:    []string{"-reenable", "https://example.com/non-existent.xml"},
			wantErr: errNoFeed,
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
				f              = testFetcher(t, testMux(t, nil))
				stdout, stderr bytes.Buffer
			)

			err := f.main(context.Background(), tc.args, getenvFunc(tc.env), &stdout, &stderr)

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
				tc.checkFunc(t, f)
			}
		})
	}
}

func TestListFeeds(t *testing.T) {
	t.Parallel()

	testutil.RunGolden(t, "testdata/list/*.txtar", func(t *testing.T, tc string) []byte {
		t.Parallel()

		tm := testMux(t, nil)
		tm.gist = txtarToGist(t, readFile(t, tc))
		f := testFetcher(t, tm)

		var buf bytes.Buffer
		if err := f.listFeeds(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}

		return buf.Bytes()
	}, *update)
}

//go:embed testdata/feed.xml
var atomFeed []byte

func TestRun(t *testing.T) {
	t.Parallel()
	f := testFetcher(t, testMux(t, map[string]http.HandlerFunc{
		"GET example.com/feed.xml": func(w http.ResponseWriter, r *http.Request) {
			w.Write(atomFeed)
		},
	}))
	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFailingFeed(t *testing.T) {
	t.Parallel()

	tm := testMux(t, map[string]http.HandlerFunc{
		"GET example.com/feed.xml": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "I'm a teapot.", http.StatusTeapot)
		},
	})
	f := testFetcher(t, tm)
	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}

	state := tm.state(t)

	testutil.AssertEqual(t, state["https://example.com/feed.xml"].ErrorCount, 1)
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].LastError, "want 200, got 418: I'm a teapot.\n")
}

func TestDisablingAndReenablingFailingFeed(t *testing.T) {
	t.Parallel()

	tm := testMux(t, map[string]http.HandlerFunc{
		"GET example.com/feed.xml": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "I'm a teapot.", http.StatusTeapot)
		},
	})

	f := testFetcher(t, tm)

	const attempts = errorThreshold
	for range attempts {
		if err := f.run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	state1 := tm.state(t)

	testutil.AssertEqual(t, state1["https://example.com/feed.xml"].Disabled, true)
	testutil.AssertEqual(t, state1["https://example.com/feed.xml"].ErrorCount, attempts)
	testutil.AssertEqual(t, state1["https://example.com/feed.xml"].LastError, "want 200, got 418: I'm a teapot.\n")

	testutil.AssertEqual(t, len(tm.sentMessages), 1)
	testutil.AssertEqual(t, tm.sentMessages[0]["text"], "❌ Something went wrong:\n<pre><code>"+html.EscapeString("fetching feed \"https://example.com/feed.xml\" failed after 12 previous attempts: want 200, got 418: I'm a teapot.\n; feed was disabled, to reenable it run 'tgfeed -reenable \"https://example.com/feed.xml\"'")+"</code></pre>")

	if err := f.reenable(context.Background(), "https://example.com/feed.xml"); err != nil {
		t.Fatal(err)
	}
	state2 := tm.state(t)
	testutil.AssertEqual(t, state2["https://example.com/feed.xml"].Disabled, false)
	testutil.AssertEqual(t, state2["https://example.com/feed.xml"].ErrorCount, 0)
	testutil.AssertEqual(t, state2["https://example.com/feed.xml"].LastError, "")
}

var (
	//go:embed testdata/load/gist.json
	gistJSON []byte

	//go:embed testdata/load/gist_error.json
	gistErrorJSON []byte
)

func TestLoadFromGist(t *testing.T) {
	t.Parallel()

	tm := testMux(t, nil)
	tm.gist = gistJSON
	f := testFetcher(t, tm)

	if err := f.loadFromGist(context.Background()); err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, f.errorTemplate, "test")
}

func TestLoadFromGistHandleError(t *testing.T) {
	t.Parallel()

	tm := testMux(t, map[string]http.HandlerFunc{
		"GET api.github.com/gists/test": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write(gistErrorJSON)
		},
	})
	f := testFetcher(t, tm)
	err := f.loadFromGist(context.Background())
	testutil.AssertEqual(t, err.Error(), fmt.Sprintf("GET \"https://api.github.com/gists/test\": want 200, got 404: %s", gistErrorJSON))
}

func TestFetchWithIfModifiedSinceAndETag(t *testing.T) {
	t.Parallel()

	const (
		ifModifiedSince = "Tue, 25 Jun 2024 12:00:00 GMT"
		eTag            = "test"
	)

	tm := testMux(t, map[string]http.HandlerFunc{
		"GET example.com/feed.xml": func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("If-Modified-Since") == ifModifiedSince && r.Header.Get("If-None-Match") == eTag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("Last-Modified", ifModifiedSince)
			w.Header().Set("ETag", eTag)
			w.Write(atomFeed)
		},
	})
	f := testFetcher(t, tm)

	// Initial fetch, should update state with Last-Modified and ETag.
	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}

	state1 := tm.state(t)

	testutil.AssertEqual(t, state1["https://example.com/feed.xml"].LastModified, ifModifiedSince)
	testutil.AssertEqual(t, state1["https://example.com/feed.xml"].ETag, eTag)
	testutil.AssertEqual(t, f.stats.NotModifiedFeeds, 0)

	// Second fetch, should use If-Modified-Since and ETag and get 304.
	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}

	state2 := tm.state(t)

	testutil.AssertEqual(t, state2["https://example.com/feed.xml"].LastModified, ifModifiedSince)
	testutil.AssertEqual(t, state2["https://example.com/feed.xml"].ETag, eTag)
	testutil.AssertEqual(t, f.stats.NotModifiedFeeds, 1)
}

//go:embed testdata/serviceaccount.json
var testKey []byte

func TestReportStats(t *testing.T) {
	t.Parallel()

	wantValues := [][]string{
		{
			"1", "1", "0", "0",
			"2024-06-22T12:00:00Z", "1s", "1", "1s", "0s", "1000",
		},
	}

	tm := testMux(t, map[string]http.HandlerFunc{
		"POST sheets.googleapis.com/v4/spreadsheets/test/values/Stats:append": func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Values [][]string `json:"values"`
			}
			readJSON(t, r.Body, &req)
			testutil.AssertEqual(t, req.Values, wantValues)
			w.Write([]byte("{}"))
		},
		`POST oauth2.googleapis.com/token`: func(w http.ResponseWriter, r *http.Request) {
			// Assume that authentication always succeeds.
			var response struct {
				AccessToken string `json:"access_token"`
			}
			response.AccessToken = "foobar"
			web.RespondJSON(w, response)
		},
	})
	f := testFetcher(t, tm)

	f.statsSpreadsheetID = "test"
	var err error
	f.serviceAccountKey, err = serviceaccount.LoadKey(testKey)
	if err != nil {
		t.Fatal(err)
	}

	f.stats = &stats{
		TotalFeeds:       1,
		SuccessFeeds:     1,
		StartTime:        time.Date(2024, 6, 22, 12, 0, 0, 0, time.UTC),
		Duration:         time.Second,
		TotalItemsParsed: 1,
		TotalFetchTime:   time.Second,
		MemoryUsage:      1000,
	}

	if err := f.reportStats(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, r io.Reader, v any) {
	if err := json.NewDecoder(r).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testFetcher(t *testing.T, m *mux) *fetcher {
	f := &fetcher{
		httpc:   testutil.MockHTTPClient(m.mux),
		logf:    t.Logf,
		ghToken: "superdupersecret",
		gistID:  "test",
		tgToken: tgToken,
		chatID:  "test",
	}
	f.initOnce.Do(f.doInit)
	return f
}

type mux struct {
	mux          *http.ServeMux
	mu           sync.Mutex
	gist         []byte
	sentMessages []map[string]any
}

func (m *mux) state(t *testing.T) map[string]*feedState {
	m.mu.Lock()
	defer m.mu.Unlock()
	updatedGist := testutil.UnmarshalJSON[*gist.Gist](t, m.gist)
	stateJSON, ok := updatedGist.Files["state.json"]
	if !ok {
		t.Fatal("state.json has not found in updated gist")
	}
	return testutil.UnmarshalJSON[map[string]*feedState](t, []byte(stateJSON.Content))
}

const (
	getGist      = "GET api.github.com/gists/test"
	patchGist    = "PATCH api.github.com/gists/test"
	sendTelegram = "POST api.telegram.org/{token}/sendMessage"
)

func testMux(t *testing.T, overrides map[string]http.HandlerFunc) *mux {
	m := &mux{mux: http.NewServeMux()}
	m.mux.HandleFunc(getGist, orHandler(overrides[getGist], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, r.Header.Get("Authorization"), "Bearer superdupersecret")
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.gist != nil {
			w.Write(m.gist)
			return
		}
		w.Write(txtarToGist(t, gistTxtar))
	}))
	m.mux.HandleFunc(patchGist, orHandler(overrides[patchGist], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, r.Header.Get("Authorization"), "Bearer superdupersecret")
		m.mu.Lock()
		defer m.mu.Unlock()
		m.gist = read(t, r.Body)
		w.Write(m.gist)
	}))
	m.mux.HandleFunc(sendTelegram, orHandler(overrides[sendTelegram], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
		m.mu.Lock()
		defer m.mu.Unlock()
		sentMessage := read(t, r.Body)
		m.sentMessages = append(m.sentMessages, testutil.UnmarshalJSON[map[string]any](t, sentMessage))
		w.Write([]byte("{}"))
	}))
	for pat, h := range overrides {
		if pat == getGist || pat == patchGist || pat == sendTelegram {
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

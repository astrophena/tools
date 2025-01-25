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
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/cli/clitest"
	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/api/google/serviceaccount"
	"go.astrophena.name/tools/internal/util/rr"
	"go.astrophena.name/tools/internal/web"
)

var update = flag.Bool("update", false, "update golden files in testdata")

// Typical Telegram Bot API token, copied from docs.
const tgToken = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

var (
	//go:embed testdata/gists/default.txtar
	defaultGistTxtar []byte

	//go:embed testdata/gists/github_notifications.txtar
	githubNotificationsTxtar []byte
)

var (
	//go:embed testdata/feed.xml
	atomFeed []byte

	atomFeedRoute = "GET example.com/feed.xml"
	atomFeedURL   = "https://example.com/feed.xml"
)

func TestFetcherMain(t *testing.T) {
	t.Parallel()

	clitest.Run(t, func(t *testing.T) *fetcher {
		return testFetcher(t, testMux(t, map[string]http.HandlerFunc{
			atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
				w.Write(atomFeed)
			},
		}))
	}, map[string]clitest.Case[*fetcher]{
		"returns an error without flags": {
			Args:    []string{},
			WantErr: cli.ErrInvalidArgs,
		},
		"run": {
			Args:               []string{"-run"},
			WantNothingPrinted: true,
		},
		"run (dry)": {
			Args: []string{"-run", "-dry"},
		},
		"version": {
			Args:    []string{"-version"},
			WantErr: cli.ErrExitVersion,
		},
		"edit without any changes": {
			Args: []string{"-edit"},
			Env: map[string]string{
				"EDITOR": "true",
			},
			WantInStderr: "No changes made to config.star, not doing anything.",
		},
		"edit without defined editor": {
			Args:    []string{"-edit"},
			WantErr: errNoEditor,
		},
		"list feeds": {
			Args: []string{"-feeds"},
		},
		"reenable disabled feed": {
			Args:               []string{"-reenable", "https://example.com/disabled.xml"},
			WantNothingPrinted: true,
			CheckFunc: func(t *testing.T, f *fetcher) {
				f.state.RAccess(func(s map[string]*feedState) {
					testutil.AssertEqual(t, s["https://example.com/disabled.xml"].Disabled, false)
				})
			},
		},
		"reenable non-existent feed": {
			Args:    []string{"-reenable", "https://example.com/non-existent.xml"},
			WantErr: errNoFeed,
		},
	},
	)
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

func TestFailingFeed(t *testing.T) {
	t.Parallel()

	tm := testMux(t, map[string]http.HandlerFunc{
		atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "I'm a teapot.", http.StatusTeapot)
		},
	})
	f := testFetcher(t, tm)
	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}

	state := tm.state(t)

	testutil.AssertEqual(t, state[atomFeedURL].ErrorCount, 1)
	testutil.AssertEqual(t, state[atomFeedURL].LastError, "want 200, got 418: I'm a teapot.\n")
}

func TestDisablingAndReenablingFailingFeed(t *testing.T) {
	t.Parallel()

	tm := testMux(t, map[string]http.HandlerFunc{
		atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
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

	testutil.AssertEqual(t, state1[atomFeedURL].Disabled, true)
	testutil.AssertEqual(t, state1[atomFeedURL].ErrorCount, attempts)
	testutil.AssertEqual(t, state1[atomFeedURL].LastError, "want 200, got 418: I'm a teapot.\n")

	testutil.AssertEqual(t, len(tm.sentMessages), 1)
	testutil.AssertEqual(t, tm.sentMessages[0]["text"], "❌ Something went wrong:\n<pre><code>"+html.EscapeString("fetching feed \"https://example.com/feed.xml\" failed after 12 previous attempts: want 200, got 418: I'm a teapot.\n; feed was disabled, to reenable it run 'tgfeed -reenable \"https://example.com/feed.xml\"'")+"</code></pre>")

	if err := f.reenable(context.Background(), atomFeedURL); err != nil {
		t.Fatal(err)
	}
	state2 := tm.state(t)
	testutil.AssertEqual(t, state2[atomFeedURL].Disabled, false)
	testutil.AssertEqual(t, state2[atomFeedURL].ErrorCount, 0)
	testutil.AssertEqual(t, state2[atomFeedURL].LastError, "")
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
		atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
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

	testutil.AssertEqual(t, state1[atomFeedURL].LastModified, ifModifiedSince)
	testutil.AssertEqual(t, state1[atomFeedURL].ETag, eTag)
	f.stats.Access(func(s *stats) {
		testutil.AssertEqual(t, s.NotModifiedFeeds, 0)
	})

	// Second fetch, should use If-Modified-Since and ETag and get 304.
	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}

	state2 := tm.state(t)

	testutil.AssertEqual(t, state2[atomFeedURL].LastModified, ifModifiedSince)
	testutil.AssertEqual(t, state2[atomFeedURL].ETag, eTag)
	f.stats.Access(func(s *stats) {
		testutil.AssertEqual(t, s.NotModifiedFeeds, 1)
	})
}

var (
	//go:embed testdata/serviceaccount.json
	testKeyJSON []byte
	testKey     = must(serviceaccount.LoadKey(testKeyJSON))
)

func must[T any](val T, err error) T {
	if err != nil {
		panic(err)
	}
	return val
}

func TestFeedString(t *testing.T) {
	f := &feed{URL: atomFeedURL}
	testutil.AssertEqual(t, f.String(), fmt.Sprintf("<feed url=%q>", atomFeedURL))
}

func TestParseConfig(t *testing.T) {
	testutil.RunGolden(t, "testdata/config/*.star", func(t *testing.T, match string) []byte {
		config := readFile(t, match)

		tm := testMux(t, nil)

		ar := &txtar.Archive{
			Files: []txtar.File{
				{Name: "config.star", Data: config},
			},
		}
		tm.gist = txtarToGist(t, txtar.Format(ar))

		f := testFetcher(t, tm)
		if err := f.run(context.Background()); err != nil {
			return []byte(fmt.Sprintf("Error: %v", err))
		}

		return toJSON(t, f.feeds)
	}, *update)
}

//go:embed testdata/rules/feed.xml
var rulesAtomFeed []byte

func TestBlockAndKeepRules(t *testing.T) {
	t.Parallel()

	testutil.RunGolden(t, "testdata/rules/*.star", func(t *testing.T, match string) []byte {
		t.Parallel()

		config := readFile(t, match)

		tm := testMux(t, map[string]http.HandlerFunc{
			atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(rulesAtomFeed))
			},
		})

		state := map[string]*feedState{
			"https://example.com/feed.xml": {
				LastUpdated: time.Time{},
			},
		}
		ar := &txtar.Archive{
			Files: []txtar.File{
				{Name: "config.star", Data: config},
				{Name: "state.json", Data: toJSON(t, state)},
			},
		}
		tm.gist = txtarToGist(t, txtar.Format(ar))

		f := testFetcher(t, tm)
		if err := f.run(context.Background()); err != nil {
			t.Fatal(err)
		}

		sort.SliceStable(tm.sentMessages, func(i, j int) bool {
			return compareMaps(tm.sentMessages[i], tm.sentMessages[j])
		})
		return toJSON(t, tm.sentMessages)
	}, *update)
}

func compareMaps(map1, map2 map[string]any) bool {
	text1, ok1 := map1["text"].(string)
	text2, ok2 := map2["text"].(string)
	if !ok1 {
		if !ok2 {
			// Both don't have text, consider them equal (no change in order).
			return false
		}
		// map1 doesn't have text, map2 does, so map2 comes later.
		return false
	}
	if !ok2 {
		// map1 has text, map2 doesn't, so map1 comes earlier
		return true
	}
	// Compare texts alphabetically.
	return text1 < text2
}

func readFile(t *testing.T, path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGitHubNotificationsFeed(t *testing.T) {
	t.Parallel()

	rec, err := rr.Open(filepath.Join("internal", "ghnotify", "testdata", "handler.httprr"), http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	tm := testMux(t, nil)
	tm.gist = txtarToGist(t, githubNotificationsTxtar)
	f := testFetcher(t, tm)
	f.httpc = &http.Client{
		Transport: &roundTripper{f.httpc.Transport, rec.Client().Transport},
	}

	if err := f.run(cli.WithEnv(context.Background(), &cli.Env{
		Stderr: logger.Logf(t.Logf),
	})); err != nil {
		t.Fatal(err)
	}

	state := tm.state(t)["tgfeed://github-notifications"]
	testutil.AssertEqual(t, state.ErrorCount, 0)
	testutil.AssertEqual(t, state.LastError, "")
}

type roundTripper struct{ main, notifications http.RoundTripper }

func (rt *roundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host == "api.github.com" && r.URL.Path == "/notifications" {
		r.Header.Del("Authorization")
		return rt.notifications.RoundTrip(r)
	}
	return rt.main.RoundTrip(r)
}

func testFetcher(t *testing.T, m *mux) *fetcher {
	f := &fetcher{
		httpc:              testutil.MockHTTPClient(m.mux),
		logf:               t.Logf,
		ghToken:            "superdupersecret",
		gistID:             "test",
		tgToken:            tgToken,
		chatID:             "test",
		statsSpreadsheetID: "test",
		serviceAccountKey:  testKey,
	}
	f.init.Do(f.doInit)
	return f
}

type mux struct {
	mux          *http.ServeMux
	mu           sync.Mutex
	gist         []byte
	sentMessages []map[string]any
	statsValues  [][]string
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
	getGist        = "GET api.github.com/gists/test"
	patchGist      = "PATCH api.github.com/gists/test"
	sendTelegram   = "POST api.telegram.org/{token}/sendMessage"
	updateSheet    = "POST sheets.googleapis.com/v4/spreadsheets/test/values/Stats:append"
	getGoogleToken = "POST oauth2.googleapis.com/token"
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
		w.Write(txtarToGist(t, defaultGistTxtar))
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
	m.mux.HandleFunc(updateSheet, orHandler(overrides[updateSheet], func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		var req struct {
			Values [][]string `json:"values"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		m.statsValues = req.Values
		w.Write([]byte("{}"))
	}))
	m.mux.HandleFunc(getGoogleToken, orHandler(overrides[getGoogleToken], func(w http.ResponseWriter, r *http.Request) {
		// Assume that authentication always succeeds.
		var response struct {
			AccessToken string `json:"access_token"`
		}
		response.AccessToken = "foobar"
		web.RespondJSON(w, response)
	}))
	for pat, h := range overrides {
		if pat == getGist || pat == patchGist || pat == sendTelegram || pat == updateSheet || pat == getGoogleToken {
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

func toJSON(t *testing.T, val any) []byte {
	b, err := json.MarshalIndent(val, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

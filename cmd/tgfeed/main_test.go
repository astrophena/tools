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
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/cli/clitest"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/api/google/serviceaccount"
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

func must[T any](val T, err error) T {
	if err != nil {
		panic(err)
	}
	return val
}

func readFile(t *testing.T, path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var (
	//go:embed testdata/serviceaccount.json
	testKeyJSON []byte
	testKey     = must(serviceaccount.LoadKey(testKeyJSON))
)

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

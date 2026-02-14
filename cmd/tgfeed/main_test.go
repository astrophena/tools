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
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/cli/clitest"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.astrophena.name/tools/cmd/tgfeed/internal/state"
)

var updateGolden = flag.Bool("update", false, "update golden files in testdata")

// Typical Telegram Bot API token, copied from docs.
const tgToken = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

var (
	//go:embed testdata/data/default.txtar
	defaultTxtar []byte
	//go:embed testdata/data/github_notifications.txtar
	githubNotificationsTxtar []byte

	//go:embed testdata/feeds/atom.xml
	atomFeed      []byte
	atomFeedRoute = "GET example.com/feed.xml"
	atomFeedURL   = "https://example.com/feed.xml"
)

func TestFetcherMain(t *testing.T) {
	t.Parallel()

	clitest.Run(t, func(t *testing.T) *fetcher {
		return testFetcher(t, testMux(t, txtarToFS(txtar.Parse(defaultTxtar)), map[string]http.HandlerFunc{
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
			Args:               []string{"run"},
			WantNothingPrinted: true,
		},
		"run (dry)": {
			Args: []string{"-dry", "run"},
		},
		"version": {
			Args:    []string{"-version"},
			WantErr: cli.ErrExitVersion,
		},
		"edit without defined editor": {
			Args:    []string{"edit"},
			WantErr: errNoEditor,
		},
		"list feeds": {
			Args: []string{"feeds"},
		},
		"reenable command without arguments": {
			Args:    []string{"reenable"},
			WantErr: cli.ErrInvalidArgs,
		},
		"reenable disabled feed": {
			Args:               []string{"reenable", "https://example.com/disabled.xml"},
			WantNothingPrinted: true,
			CheckFunc: func(t *testing.T, f *fetcher) {
				st, ok := f.getState("https://example.com/disabled.xml")
				testutil.AssertEqual(t, ok, true)
				testutil.AssertEqual(t, st.Disabled, false)
			},
		},
		"reenable non-existent feed": {
			Args:    []string{"reenable", "https://example.com/non-existent.xml"},
			WantErr: errNoFeed,
		},
	},
	)
}

func TestListFeeds(t *testing.T) {
	t.Parallel()

	testutil.RunGolden(t, "testdata/list/*.txtar", func(t *testing.T, tc string) []byte {
		t.Parallel()

		tm := testMux(t, txtarToFS(txtar.Parse(readFile(t, tc))), nil)
		f := testFetcher(t, tm)

		var buf bytes.Buffer
		if err := f.listFeeds(t.Context(), &buf); err != nil {
			t.Fatal(err)
		}

		return buf.Bytes()
	}, *updateGolden)
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testFetcher(t *testing.T, m *mux) *fetcher {
	f := &fetcher{
		httpc:    testutil.MockHTTPClient(m.mux),
		logf:     t.Logf,
		ghToken:  "superdupersecret",
		stateDir: m.stateDir,
		tgToken:  tgToken,
		chatID:   "test",
	}
	f.init.Do(func() {
		f.doInit(t.Context())
	})
	return f
}

type mux struct {
	mux          *http.ServeMux
	mu           sync.Mutex
	stateDir     string
	sentMessages []map[string]any
}

func (m *mux) state(t *testing.T) map[string]*state.Feed {
	m.mu.Lock()
	defer m.mu.Unlock()

	content, err := os.ReadFile(filepath.Join(m.stateDir, "state.json"))
	if err != nil {
		t.Fatalf("reading state.json: %v", err)
	}

	return testutil.UnmarshalJSON[map[string]*state.Feed](t, content)
}

const (
	sendTelegram = "POST api.telegram.org/{token}/sendMessage"
)

func testMux(t *testing.T, baseState fs.FS, overrides map[string]http.HandlerFunc) *mux {
	m := &mux{
		mux:      http.NewServeMux(),
		stateDir: t.TempDir(),
	}
	if baseState != nil {
		if err := os.CopyFS(m.stateDir, baseState); err != nil {
			t.Fatalf("initializing state directory: %v", err)
		}
	}
	m.mux.HandleFunc(sendTelegram, orHandler(overrides[sendTelegram], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
		m.mu.Lock()
		defer m.mu.Unlock()
		sentMessage := read(t, r.Body)
		m.sentMessages = append(m.sentMessages, testutil.UnmarshalJSON[map[string]any](t, sentMessage))
		w.Write([]byte("{}"))
	}))
	for pat, h := range overrides {
		if slices.Contains([]string{sendTelegram}, pat) {
			continue
		}
		m.mux.HandleFunc(pat, h)
	}
	return m
}

func txtarToFS(ar *txtar.Archive) fs.FS {
	fs := make(fstest.MapFS)
	for _, file := range ar.Files {
		fs[file.Name] = &fstest.MapFile{
			Data: file.Data,
		}
	}
	return fs
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

func toJSON(t *testing.T, val any) []byte {
	b, err := json.MarshalIndent(val, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSleepReturnsTrueAfterDuration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	start := time.Now()
	if !sleep(ctx, 10*time.Millisecond) {
		t.Fatal("sleep() = false, want true")
	}
	if elapsed := time.Since(start); elapsed < 10*time.Millisecond {
		t.Fatalf("sleep() elapsed = %v, want >= %v", elapsed, 10*time.Millisecond)
	}
}

func TestSleepReturnsFalseOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	if sleep(ctx, time.Second) {
		t.Fatal("sleep() = true, want false")
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("sleep() elapsed = %v, want quick return", elapsed)
	}
}

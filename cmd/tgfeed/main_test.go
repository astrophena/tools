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
		return newTestFetcher(t, newDefaultTestEnv(t, map[string]http.HandlerFunc{
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
				st, ok := f.state.Get("https://example.com/disabled.xml")
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

		env := newTestEnv(t, txtarToFS(txtar.Parse(readFile(t, tc))), nil)
		f := newTestFetcher(t, env)

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

func stateArchive(t *testing.T, config []byte, state map[string]*state.Feed) fs.FS {
	t.Helper()

	ar := &txtar.Archive{
		Files: []txtar.File{
			{Name: "config.star", Data: config},
		},
	}
	if state != nil {
		ar.Files = append(ar.Files, txtar.File{Name: "state.json", Data: toJSON(t, state)})
	}
	return txtarToFS(ar)
}

func newDefaultTestEnv(t *testing.T, overrides map[string]http.HandlerFunc) *testEnv {
	t.Helper()
	return newTestEnv(t, txtarToFS(txtar.Parse(defaultTxtar)), overrides)
}

func newTestFetcher(t *testing.T, env *testEnv) *fetcher {
	f := &fetcher{
		httpc:    testutil.MockHTTPClient(env.mux),
		logf:     t.Logf,
		ghToken:  "superdupersecret",
		stateDir: env.stateDir,
		tgToken:  tgToken,
		chatID:   "test",
	}
	f.init.Do(func() {
		f.doInit(t.Context())
	})
	return f
}

type testEnv struct {
	mux          *http.ServeMux
	mu           sync.Mutex
	stateDir     string
	sentMessages []map[string]any
}

func (env *testEnv) state(t *testing.T) map[string]*state.Feed {
	env.mu.Lock()
	defer env.mu.Unlock()

	content, err := os.ReadFile(filepath.Join(env.stateDir, "state.json"))
	if err != nil {
		t.Fatalf("reading state.json: %v", err)
	}

	return testutil.UnmarshalJSON[map[string]*state.Feed](t, content)
}

func (env *testEnv) sentText(t *testing.T, index int) string {
	t.Helper()
	env.mu.Lock()
	defer env.mu.Unlock()

	text, ok := env.sentMessages[index]["text"].(string)
	if !ok {
		t.Fatalf("sent message %d has no text field", index)
	}
	return text
}

func (env *testEnv) sortedSentMessagesJSON(t *testing.T) []byte {
	t.Helper()
	env.mu.Lock()
	defer env.mu.Unlock()

	sent := append([]map[string]any(nil), env.sentMessages...)
	slices.SortFunc(sent, compareMessages)
	return toJSON(t, sent)
}

const (
	sendTelegram = "POST api.telegram.org/{token}/sendMessage"
)

func newTestEnv(t *testing.T, baseState fs.FS, overrides map[string]http.HandlerFunc) *testEnv {
	env := &testEnv{
		mux:      http.NewServeMux(),
		stateDir: t.TempDir(),
	}
	if baseState != nil {
		if err := os.CopyFS(env.stateDir, baseState); err != nil {
			t.Fatalf("initializing state directory: %v", err)
		}
	}
	env.mux.HandleFunc(sendTelegram, orHandler(overrides[sendTelegram], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
		env.mu.Lock()
		defer env.mu.Unlock()
		sentMessage := read(t, r.Body)
		env.sentMessages = append(env.sentMessages, testutil.UnmarshalJSON[map[string]any](t, sentMessage))
		w.Write([]byte("{}"))
	}))
	for pat, h := range overrides {
		if slices.Contains([]string{sendTelegram}, pat) {
			continue
		}
		env.mux.HandleFunc(pat, h)
	}
	return env
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

func compareMessages(map1, map2 map[string]any) int {
	text1, ok1 := map1["text"].(string)
	text2, ok2 := map2["text"].(string)
	if !ok1 {
		if !ok2 {
			return 0
		}
		return 1
	}
	if !ok2 {
		return -1
	}
	return strings.Compare(text1, text2)
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

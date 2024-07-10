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
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/testutil"
	"go.astrophena.name/tools/internal/testutil/txtar"
)

// Typical Telegram Bot API token, copied from docs.
const tgToken = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

var (
	//go:embed testdata/gists/main.txtar
	gistTxtar []byte
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
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

func TestRun(t *testing.T) {
	t.Parallel()
	f := testFetcher(t, testMux(t, nil))
	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSubscribeAndUnsubscribe(t *testing.T) {
	t.Parallel()

	f := testFetcher(t, testMux(t, nil))

	const feedURL = "https://example.com/feed2.xml"

	testutil.AssertNotContains(t, f.feeds, feedURL)
	if err := f.subscribe(context.Background(), feedURL); err != nil {
		t.Fatal(err)
	}
	testutil.AssertContains(t, f.feeds, feedURL)
	if err := f.unsubscribe(context.Background(), feedURL); err != nil {
		t.Fatal(err)
	}
	testutil.AssertNotContains(t, f.feeds, feedURL)
}

func TestUnsubscribeRemovesState(t *testing.T) {
	t.Parallel()

	f := testFetcher(t, testMux(t, nil))

	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	const feedURL = "https://example.com/feed.xml"
	_, hasState := f.state[feedURL]
	if !hasState {
		t.Fatalf("f.state doesn't contain state for feed %s", feedURL)
	}

	if err := f.unsubscribe(context.Background(), feedURL); err != nil {
		t.Fatal(err)
	}
	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, hasState = f.state[feedURL]
	if hasState {
		t.Fatalf("f.state still contains state for feed %s even after unsubscribing", feedURL)
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

	updatedGist := testutil.UnmarshalJSON[*gist.Gist](t, tm.gist)
	stateJSON, ok := updatedGist.Files["state.json"]
	if !ok {
		t.Fatal("state.json has not found in updated gist")
	}
	state := testutil.UnmarshalJSON[map[string]feedState](t, []byte(stateJSON.Content))

	testutil.AssertEqual(t, state["https://example.com/feed.xml"].ErrorCount, 1)
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].LastError, "want 200, got 418")
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

	getState := func() map[string]feedState {
		updatedGist := testutil.UnmarshalJSON[*gist.Gist](t, tm.gist)
		stateJSON, ok := updatedGist.Files["state.json"]
		if !ok {
			t.Fatal("state.json has not found in updated gist")
		}
		return testutil.UnmarshalJSON[map[string]feedState](t, []byte(stateJSON.Content))
	}
	state := getState()

	testutil.AssertEqual(t, state["https://example.com/feed.xml"].Disabled, true)
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].ErrorCount, attempts)
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].LastError, "want 200, got 418")

	testutil.AssertEqual(t, len(tm.sentMessages), 1)
	testutil.AssertEqual(t, tm.sentMessages[0]["text"], "❌ Something went wrong:\n<pre><code>"+html.EscapeString("fetching feed \"https://example.com/feed.xml\" failed after 12 previous attempts: want 200, got 418; feed was disabled, to reenable it run 'tgfeed -reenable \"https://example.com/feed.xml\"'")+"</code></pre>")

	if err := f.reenable(context.Background(), "https://example.com/feed.xml"); err != nil {
		t.Fatal(err)
	}
	state = getState()
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].Disabled, false)
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].ErrorCount, 0)
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].LastError, "")
}

var (
	//go:embed testdata/load/gist.json
	gistJSON []byte

	//go:embed testdata/load/gist-error.json
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

func TestReportStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, r.Method, http.MethodPost)
		testutil.AssertEqual(t, r.URL.Path, "/")

		token := r.URL.Query().Get("token")
		testutil.AssertEqual(t, token, "test-token")

		w.Write([]byte(`{"status": "success"}`))
	}))
	defer server.Close()

	f := &fetcher{
		statsCollectorURL:   server.URL,
		statsCollectorToken: "test-token",
		stats: &stats{
			TotalFeeds:       10,
			SuccessFeeds:     8,
			FailedFeeds:      2,
			NotModifiedFeeds: 3,
			StartTime:        time.Now(),
			Duration:         duration(5 * time.Minute),
			TotalItemsParsed: 100,
			TotalFetchTime:   duration(2 * time.Minute),
			AvgFetchTime:     duration(12 * time.Second),
			MemoryUsage:      1024 * 1024,
		},
	}

	if err := f.reportStats(context.Background()); err != nil {
		t.Errorf("reportStats failed: %v", err)
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
		httpc:   testutil.MockHTTPClient(t, m.mux),
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

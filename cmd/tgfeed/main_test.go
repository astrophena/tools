package main

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"go.astrophena.name/tools/internal/testutil"

	"golang.org/x/tools/txtar"
)

// Typical Telegram Bot API token, copied from docs.
const tgToken = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

var (
	//go:embed testdata/gists/main.txtar
	gistTxtar []byte

	//go:embed testdata/gists/feed.txtar
	gistFeedTxtar []byte
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
		f := testFetcher(tm)

		var buf bytes.Buffer
		if err := f.listFeeds(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}

		return buf.Bytes()
	}, *update)
}

func TestRun(t *testing.T) {
	t.Parallel()
	f := testFetcher(testMux(t, nil))
	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFetch(t *testing.T) {
	t.Parallel()

	testutil.RunGolden(t, "testdata/feeds/*.xml.gz", func(t *testing.T, tc string) []byte {
		t.Parallel()

		b := unzip(t, readFile(t, tc))

		tm := testMux(t, map[string]http.HandlerFunc{
			"GET example.com/feed.xml": func(w http.ResponseWriter, r *http.Request) {
				w.Write(b)
			},
		})
		tm.gist = txtarToGist(t, gistFeedTxtar)
		f := testFetcher(tm)

		if err := f.run(context.Background()); err != nil {
			t.Fatal(err)
		}

		u, err := json.MarshalIndent(f.updates, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		return u
	}, *update)
}

func unzip(t *testing.T, gz []byte) []byte {
	r := bytes.NewReader(gz)
	zr, err := gzip.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	var buf bytes.Buffer
	_, err = io.Copy(&buf, zr)
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSubscribeAndUnsubscribe(t *testing.T) {
	t.Parallel()

	f := testFetcher(testMux(t, nil))

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

	f := testFetcher(testMux(t, nil))

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
	f := testFetcher(tm)
	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}

	updatedGist := testutil.UnmarshalJSON[*gist](t, tm.gist)
	stateJSON, ok := updatedGist.Files["state.json"]
	if !ok {
		t.Fatal("state.json has not found in updated gist")
	}
	state := testutil.UnmarshalJSON[map[string]*feedState](t, []byte(stateJSON.Content))

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
	f := testFetcher(tm)

	const attempts = errorThreshold
	for range attempts {
		if err := f.run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	getState := func() map[string]*feedState {
		updatedGist := testutil.UnmarshalJSON[gist](t, tm.gist)
		stateJSON, ok := updatedGist.Files["state.json"]
		if !ok {
			t.Fatal("state.json has not found in updated gist")
		}
		return testutil.UnmarshalJSON[map[string]*feedState](t, []byte(stateJSON.Content))
	}
	state := getState()

	testutil.AssertEqual(t, state["https://example.com/feed.xml"].Disabled, true)
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].ErrorCount, attempts)
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].LastError, "want 200, got 418")

	testutil.AssertEqual(t, len(tm.sentMessages), 1)
	testutil.AssertEqual(t, tm.sentMessages[0]["text"], "❌ Something went wrong:\n<pre><code>fetching feed \"https://example.com/feed.xml\" failed after 12 previous attempts: want 200, got 418; feed was disabled, to reenable it run 'tgfeed -reenable \"https://example.com/feed.xml\"'</code></pre>")

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
	f := testFetcher(tm)

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
	f := testFetcher(tm)
	err := f.loadFromGist(context.Background())
	testutil.AssertEqual(t, err.Error(), fmt.Sprintf("want 200, got 404: %s", gistErrorJSON))
}

func readFile(t *testing.T, path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type roundTripFunc func(r *http.Request) (*http.Response, error)

func (s roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return s(r)
}

func testFetcher(m *mux) *fetcher {
	return &fetcher{
		httpc: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				w := httptest.NewRecorder()
				m.mux.ServeHTTP(w, r)
				return w.Result(), nil
			}),
		},
		ghToken: "test",
		gistID:  "test",
		tgToken: tgToken,
		chatID:  "test",
	}
}

type mux struct {
	mux          *http.ServeMux
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
		testutil.AssertEqual(t, r.Header.Get("Authorization"), "Bearer test")
		if m.gist != nil {
			w.Write(m.gist)
			return
		}
		w.Write(txtarToGist(t, gistTxtar))
	}))
	m.mux.HandleFunc(patchGist, orHandler(overrides[patchGist], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, r.Header.Get("Authorization"), "Bearer test")
		m.gist = read(t, r.Body)
		w.Write(m.gist)
	}))
	m.mux.HandleFunc(sendTelegram, orHandler(overrides[sendTelegram], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
		m.sentMessages = append(m.sentMessages, testutil.UnmarshalJSON[map[string]any](t, read(t, r.Body)))
		// makeRequest tries to unmarshal response, which we didn't even use, so fool it.
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

	g := &gist{
		Files: make(map[string]*gistFile),
	}

	for _, f := range ar.Files {
		g.Files[f.Name] = &gistFile{Content: string(f.Data)}
	}

	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	return b
}

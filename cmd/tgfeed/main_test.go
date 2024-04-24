package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.astrophena.name/tools/internal/testutil"

	"golang.org/x/tools/txtar"
)

// Typical Telegram Bot API token, copied from docs.
const tgToken = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

var (
	//go:embed testdata/gist.txtar
	gistTxtar []byte

	//go:embed testdata/feed.xml
	feedXML []byte
)

func TestRun(t *testing.T) {
	t.Parallel()
	f := testFetcher(testMux(t, nil))
	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}
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

func TestDisablingFailingFeed(t *testing.T) {
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

	updatedGist := testutil.UnmarshalJSON[gist](t, tm.gist)
	stateJSON, ok := updatedGist.Files["state.json"]
	if !ok {
		t.Fatal("state.json has not found in updated gist")
	}
	state := testutil.UnmarshalJSON[map[string]*feedState](t, []byte(stateJSON.Content))

	testutil.AssertEqual(t, state["https://example.com/feed.xml"].Disabled, true)
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].ErrorCount, attempts)
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].LastError, "want 200, got 418")

	testutil.AssertEqual(t, len(tm.sentMessages), 1)
	testutil.AssertEqual(t, tm.sentMessages[0]["text"], "❌ Something went wrong:\n<pre><code>fetching feed \"https://example.com/feed.xml\" failed after 12 previous attempts: want 200, got 418; feed was disabled, to reenable it run 'tgfeed -reenable \"https://example.com/feed.xml\"'</code></pre>")
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

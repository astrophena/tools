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

func TestSubscribeAndUnsubscribe(t *testing.T) {
	f := testFetcher(testMux(t).mux)

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

func TestRun(t *testing.T) {
	f := testFetcher(testMux(t).mux)
	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFailingFeed(t *testing.T) {
	var updatedGistJSON []byte

	mux := http.NewServeMux()
	mux.HandleFunc("GET example.com/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "I'm a teapot.", http.StatusTeapot)
	})
	mux.HandleFunc("GET api.github.com/gists/test", func(w http.ResponseWriter, r *http.Request) {
		w.Write(txtarToGist(t, gistTxtar))
	})
	mux.HandleFunc("PATCH api.github.com/gists/test", func(w http.ResponseWriter, r *http.Request) {
		updatedGistJSON = read(t, r.Body)
		w.Write(updatedGistJSON)
	})
	mux.HandleFunc("POST api.telegram.org/{token}/sendMessage", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
		// Just to appease makeRequest.
		w.Write([]byte("{}"))
	})

	f := testFetcher(mux)
	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}

	updatedGist := new(gist)
	if err := json.Unmarshal(updatedGistJSON, &updatedGist); err != nil {
		t.Fatal(err)
	}
	stateJSON, ok := updatedGist.Files["state.json"]
	if !ok {
		t.Fatal("state.json has not found in updated gist")
	}
	state := testutil.UnmarshalJSON[map[string]*feedState](t, []byte(stateJSON.Content))

	testutil.AssertEqual(t, state["https://example.com/feed.xml"].ErrorCount, 1)
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].LastError, "want 200, got 418")
}

func TestDisablingFailingFeed(t *testing.T) {
	var (
		updatedGistJSON []byte
		sentMessages    []map[string]any
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET example.com/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "I'm a teapot.", http.StatusTeapot)
	})
	mux.HandleFunc("GET api.github.com/gists/test", func(w http.ResponseWriter, r *http.Request) {
		if updatedGistJSON != nil {
			w.Write(updatedGistJSON)
			return
		}
		w.Write(txtarToGist(t, gistTxtar))
	})
	mux.HandleFunc("PATCH api.github.com/gists/test", func(w http.ResponseWriter, r *http.Request) {
		updatedGistJSON = read(t, r.Body)
		w.Write(updatedGistJSON)
	})
	mux.HandleFunc("POST api.telegram.org/{token}/sendMessage", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
		sentMessages = append(sentMessages, testutil.UnmarshalJSON[map[string]any](t, read(t, r.Body)))
	})

	f := testFetcher(mux)

	const attempts = errorThreshold
	for range attempts {
		if err := f.run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	updatedGist := testutil.UnmarshalJSON[gist](t, updatedGistJSON)
	stateJSON, ok := updatedGist.Files["state.json"]
	if !ok {
		t.Fatal("state.json has not found in updated gist")
	}
	state := testutil.UnmarshalJSON[map[string]*feedState](t, []byte(stateJSON.Content))

	testutil.AssertEqual(t, state["https://example.com/feed.xml"].Disabled, true)
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].ErrorCount, attempts)
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].LastError, "want 200, got 418")

	testutil.AssertEqual(t, len(sentMessages), 1)
	testutil.AssertEqual(t, sentMessages[0]["text"], "❌ Something went wrong:\n<pre><code>fetching feed \"https://example.com/feed.xml\" failed after 12 previous attempts: want 200, got 418; feed was disabled, to reenable it run 'tgfeed -reenable \"https://example.com/feed.xml\"'</code></pre>")
}

func read(t *testing.T, r io.Reader) []byte {
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var (
	//go:embed testdata/load/gist.json
	gistJSON []byte

	//go:embed testdata/load/gist_error.json
	gistErrorJSON []byte
)

func TestLoadFromGist(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET api.github.com/gists/test", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, r.Header.Get("Authorization"), "Bearer test")
		w.Write(gistJSON)
	})

	f := testFetcher(mux)

	if err := f.loadFromGist(context.Background()); err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, f.errorTemplate, "test")
}

func TestLoadFromGistHandleError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET api.github.com/gists/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write(gistErrorJSON)
	})
	f := testFetcher(mux)
	err := f.loadFromGist(context.Background())
	testutil.AssertEqual(t, err.Error(), fmt.Sprintf("want 200, got 404: %s", gistErrorJSON))
}

type roundTripFunc func(r *http.Request) (*http.Response, error)

func (s roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return s(r)
}

func testFetcher(mux *http.ServeMux) *fetcher {
	return &fetcher{
		httpc: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
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
	mux  *http.ServeMux
	gist []byte
}

func testMux(t *testing.T) *mux {
	m := &mux{mux: http.NewServeMux()}
	m.mux.HandleFunc("GET api.github.com/gists/test", func(w http.ResponseWriter, r *http.Request) {
		if m.gist != nil {
			w.Write(m.gist)
			return
		}
		w.Write(txtarToGist(t, gistTxtar))
	})
	m.mux.HandleFunc("PATCH api.github.com/gists/test", func(w http.ResponseWriter, r *http.Request) {
		m.gist = read(t, r.Body)
		w.Write(m.gist)
	})
	m.mux.HandleFunc("POST api.telegram.org/{token}/sendMessage", func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
		// makeRequest tries to unmarshal response, which we didn't even use, so fool it.
		w.Write([]byte("{}"))
	})
	return m
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

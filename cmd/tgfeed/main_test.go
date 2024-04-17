package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/tools/internal/testutil"

	"golang.org/x/tools/txtar"
)

var update = flag.Bool("update", false, "update golden files in testdata")

// Typical Telegram Bot API token, copied from docs.
const tgToken = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

var (
	//go:embed testdata/gist.txtar
	gistTxtar []byte

	//go:embed testdata/feed.xml
	feedXML []byte
)

func TestMain(m *testing.M) {
	inTest = true
	flag.Parse()
	os.Exit(m.Run())
}

func TestRun(t *testing.T) {
	cases := map[string]struct {
		failSending bool
	}{
		"succeeds": {
			failSending: false,
		},
		"fails to send message": {
			failSending: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET example.com/feed.xml", func(w http.ResponseWriter, r *http.Request) {
				w.Write(feedXML)
			})
			mux.HandleFunc("GET api.github.com/gists/test", func(w http.ResponseWriter, r *http.Request) {
				w.Write(txtarToGist(t, gistTxtar))
			})
			mux.HandleFunc("PATCH api.github.com/gists/test", func(w http.ResponseWriter, r *http.Request) {
				w.Write(read(t, r.Body))
			})
			mux.HandleFunc("POST api.telegram.org/{token}/sendMessage", func(w http.ResponseWriter, r *http.Request) {
				testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
				if tc.failSending {
					w.WriteHeader(http.StatusInternalServerError)
				}
				// Just to appease makeRequest.
				w.Write([]byte("{}"))
			})

			f := testFetcher(mux, io.Discard)

			if err := f.run(context.Background()); err != nil && !tc.failSending {
				t.Fatal(err)
			}
		})
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

	f := testFetcher(mux, io.Discard)
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
	state := unmarshal[map[string]*feedState](t, []byte(stateJSON.Content))

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
		sentMessages = append(sentMessages, unmarshal[map[string]any](t, read(t, r.Body)))
	})

	f := testFetcher(mux, io.Discard)

	const attempts = errorThreshold + 2
	for range attempts {
		if err := f.run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	updatedGist := unmarshal[gist](t, updatedGistJSON)
	stateJSON, ok := updatedGist.Files["state.json"]
	if !ok {
		t.Fatal("state.json has not found in updated gist")
	}
	state := unmarshal[map[string]*feedState](t, []byte(stateJSON.Content))

	testutil.AssertEqual(t, state["https://example.com/feed.xml"].Disabled, true)
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].ErrorCount, attempts-1)
	testutil.AssertEqual(t, state["https://example.com/feed.xml"].LastError, "want 200, got 418")

	testutil.AssertEqual(t, len(sentMessages), 1)
	testutil.AssertEqual(t, sentMessages[0]["text"], "❌ Something went wrong:\n<pre><code>fetching feed \"https://example.com/feed.xml\" failed after 13 previous attempts: want 200, got 418; feed was disabled, to reenable it run 'tgfeed -reenable \"https://example.com/feed.xml\"'</code></pre>")
}

func read(t *testing.T, r io.Reader) []byte {
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func unmarshal[V any](t *testing.T, b []byte) V {
	var v V
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDryRun(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET example.com/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Write(feedXML)
	})
	mux.HandleFunc("GET api.github.com/gists/test", func(w http.ResponseWriter, r *http.Request) {
		w.Write(txtarToGist(t, gistTxtar))
	})

	lbuf := new(bytes.Buffer)

	f := testFetcher(mux, lbuf)
	f.dryRun = true

	if err := f.run(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := lbuf.Bytes()

	wantFile := filepath.Join("testdata", "dry_run.golden")
	if *update {
		if err := os.WriteFile(wantFile, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(wantFile)
	if err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, string(want), string(got))
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

	f := testFetcher(mux, io.Discard)

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
	f := testFetcher(mux, io.Discard)
	err := f.loadFromGist(context.Background())
	testutil.AssertEqual(t, err.Error(), fmt.Sprintf("want 200, got 404: %s", gistErrorJSON))
}

type roundTripFunc func(r *http.Request) (*http.Response, error)

func (s roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return s(r)
}

func testFetcher(mux *http.ServeMux, lw io.Writer) *fetcher {
	return &fetcher{
		httpc: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				return w.Result(), nil
			}),
		},
		log:     log.New(lw, "", 0),
		ghToken: "test",
		gistID:  "test",
		tgToken: tgToken,
		chatID:  "test",
	}
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

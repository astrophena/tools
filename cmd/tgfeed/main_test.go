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
				// We don't care about the output, so return nothing.
			})
			mux.HandleFunc("POST api.telegram.org/{token}/sendMessage", func(w http.ResponseWriter, r *http.Request) {
				testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
				if tc.failSending {
					w.WriteHeader(http.StatusInternalServerError)
				}
			})

			f := testFetcher(mux, io.Discard)

			if err := f.run(context.Background()); err != nil && !tc.failSending {
				t.Fatal(err)
			}
		})
	}
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

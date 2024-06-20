package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"go.astrophena.name/tools/internal/client/gist"
	"go.astrophena.name/tools/internal/httplogger"
	"go.astrophena.name/tools/internal/httputil"
	"go.astrophena.name/tools/internal/testutil"
	"go.astrophena.name/tools/internal/txtar"
	"go.astrophena.name/tools/internal/web"
)

// Typical Telegram Bot API token, copied from docs.
const tgToken = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

//go:embed testdata/gist.txtar
var gistTxtar []byte

func TestHealth(t *testing.T) {
	e := testEngine(t, testMux(t, nil))
	health, err := httputil.MakeJSONRequest[web.HealthResponse](context.Background(), httputil.RequestParams{
		Method:     http.MethodGet,
		URL:        "/health",
		HTTPClient: testutil.MockHTTPClient(t, e.mux),
	})
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, health.OK, true)
}

func testEngine(t *testing.T, m *mux) *engine {
	e := &engine{
		ghToken:  "test",
		gistID:   "test",
		httpc:    testutil.MockHTTPClient(t, m.mux),
		tgOwner:  123456789,
		tgSecret: "test",
		tgToken:  "test",
	}
	if os.Getenv("HTTPLOG") == "1" {
		e.httpc.Transport = httplogger.New(e.httpc.Transport, t.Logf)
	}
	e.init.Do(e.doInit)
	return e
}

type mux struct {
	mux          *http.ServeMux
	mu           sync.Mutex
	gist         []byte
	sentMessages []map[string]any
}

const (
	getGist      = "GET api.github.com/gists/test"
	sendTelegram = "POST api.telegram.org/{token}/sendMessage"
)

func testMux(t *testing.T, overrides map[string]http.HandlerFunc) *mux {
	m := &mux{mux: http.NewServeMux()}
	m.mux.HandleFunc(getGist, orHandler(overrides[getGist], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, r.Header.Get("Authorization"), "Bearer test")
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.gist != nil {
			w.Write(m.gist)
			return
		}
		w.Write(txtarToGist(t, gistTxtar))
	}))
	m.mux.HandleFunc(sendTelegram, orHandler(overrides[sendTelegram], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
		m.mu.Lock()
		defer m.mu.Unlock()
		sentMessage := read(t, r.Body)
		m.sentMessages = append(m.sentMessages, testutil.UnmarshalJSON[map[string]any](t, sentMessage))
	}))
	for pat, h := range overrides {
		if pat == getGist || pat == sendTelegram {
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

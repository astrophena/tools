package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/request"
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
	health, err := request.MakeJSON[web.HealthResponse](context.Background(), request.Params{
		Method:     http.MethodGet,
		URL:        "/health",
		HTTPClient: testutil.MockHTTPClient(t, e.mux),
	})
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, health.OK, true)
}

func TestHandleTelegramWebhook(t *testing.T) {
	tm := testMux(t, nil)
	e := testEngine(t, tm)

	testutil.Run(t, "testdata/updates/*.txt", func(t *testing.T, match string) {
		ar, err := txtar.ParseFile(match)
		if err != nil {
			t.Fatal(err)
		}

		if len(ar.Files) != 1 || ar.Files[0].Name != "update.json" {
			t.Fatalf("%s txtar should contain only one file named update.json", match)
		}

		var update json.RawMessage
		for _, f := range ar.Files {
			if f.Name == "update.json" {
				update = json.RawMessage(f.Data)
			}
		}

		_, err = request.MakeJSON[any](context.Background(), request.Params{
			Method: http.MethodPost,
			URL:    "/telegram",
			Body:   update,
			Headers: map[string]string{
				"X-Telegram-Bot-Api-Secret-Token": e.tgSecret,
			},
			HTTPClient: testutil.MockHTTPClient(t, e.mux),
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func testEngine(t *testing.T, m *mux) *engine {
	e := &engine{
		ghToken:  "test",
		gistID:   "test",
		httpc:    testutil.MockHTTPClient(t, m.mux),
		tgOwner:  123456789,
		tgSecret: "test",
		tgToken:  tgToken,
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
		web.RespondJSON(w, struct{}{})
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

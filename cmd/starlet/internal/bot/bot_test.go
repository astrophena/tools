// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package bot_test

import (
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/cmd/starlet/internal/bot"
	"go.astrophena.name/tools/internal/api/github/gist"
	"go.astrophena.name/tools/internal/starlark/lib/kvcache"
)

// Typical Telegram Bot API token, copied from docs.
const tgToken = "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"

var update = flag.Bool("update", false, "update golden files in testdata")

func TestHandleTelegramWebhook(t *testing.T) {
	t.Parallel()

	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, match string) []byte {
		t.Parallel()

		ar, err := txtar.ParseFile(match)
		if err != nil {
			t.Fatal(err)
		}

		if len(ar.Files) == 0 || ar.Files[0].Name != "bot.star" {
			t.Fatalf("%s txtar should contain at least one file: bot.star", match)
		}

		var upd json.RawMessage

		comment := strings.TrimSuffix(string(ar.Comment), "\n")
		if !strings.HasPrefix(comment, "update: ") {
			t.Fatalf("%s txtar should have a comment containing the \"update: \" string", match)
		}
		file := strings.TrimPrefix(comment, "update: ")
		b, err := os.ReadFile(filepath.Join("testdata", "updates", file+".json"))
		if err != nil {
			t.Fatal(err)
		}
		upd = json.RawMessage(b)

		tm := testMux(t, nil)
		tm.gist = txtarToGist(t, readFile(t, match))
		bot := testBot(t, tm)

		mux := http.NewServeMux()
		mux.HandleFunc("/telegram", bot.HandleTelegramWebhook)

		_, err = request.Make[request.IgnoreResponse](t.Context(), request.Params{
			Method: http.MethodPost,
			URL:    "/telegram",
			Body:   upd,
			Headers: map[string]string{
				"X-Telegram-Bot-Api-Secret-Token": "test",
			},
			HTTPClient: testutil.MockHTTPClient(mux),
		})
		if err != nil {
			t.Fatal(err)
		}

		calls, err := json.MarshalIndent(tm.telegramCalls, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return calls
	}, *update)
}

func testBot(t *testing.T, m *mux) *bot.Bot {
	t.Helper()
	b := bot.New(bot.Opts{
		GistID:     "test",
		Token:      tgToken,
		Secret:     "test",
		Owner:      123456789,
		HTTPClient: testutil.MockHTTPClient(m.mux),
		GistClient: &gist.Client{
			Token:      "test",
			HTTPClient: testutil.MockHTTPClient(m.mux),
		},
		KVCache: kvcache.Module(t.Context(), 1*time.Minute),
		Logf:    t.Logf,
	})
	if err := b.LoadFromGist(t.Context()); err != nil {
		t.Fatal(err)
	}
	return b
}

type mux struct {
	mux           *http.ServeMux
	mu            sync.Mutex
	gist          []byte
	telegramCalls []call
}

type call struct {
	Method string         `json:"method"`
	Args   map[string]any `json:"args"`
}

const (
	getGist       = "GET api.github.com/gists/test"
	getMeTelegram = "GET api.telegram.org/{token}/getMe"
	postTelegram  = "POST api.telegram.org/{token}/{method}"
)

func testMux(t *testing.T, overrides map[string]http.HandlerFunc) *mux {
	m := &mux{mux: http.NewServeMux()}
	m.gist = txtarToGist(t, []byte(`
-- bot.star --
print("hello")
`))
	m.mux.HandleFunc(getGist, orHandler(overrides[getGist], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, r.Header.Get("Authorization"), "Bearer test")
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.gist != nil {
			w.Write(m.gist)
		}
	}))
	m.mux.HandleFunc(getMeTelegram, orHandler(overrides[getMeTelegram], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
		var resp struct {
			OK     bool `json:"ok"`
			Result struct {
				ID       int64  `json:"id"`
				Username string `json:"username"`
			} `json:"result"`
		}
		resp.OK = true
		resp.Result.ID = 123456789
		resp.Result.Username = "foo_bot"
		web.RespondJSON(w, resp)
	}))
	m.mux.HandleFunc(postTelegram, orHandler(overrides[postTelegram], func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, tgToken, strings.TrimPrefix(r.PathValue("token"), "bot"))
		m.mu.Lock()
		defer m.mu.Unlock()
		b := read(t, r.Body)
		m.telegramCalls = append(m.telegramCalls, call{
			Method: r.PathValue("method"),
			Args:   testutil.UnmarshalJSON[map[string]any](t, b),
		})
		jsonOK(w)
	}))
	for pat, h := range overrides {
		if pat == getGist || pat == postTelegram || pat == getMeTelegram {
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

func readFile(t *testing.T, path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
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

func jsonOK(w http.ResponseWriter) {
	var res struct {
		Status string `json:"status"`
	}
	res.Status = "success"
	web.RespondJSON(w, res)
}

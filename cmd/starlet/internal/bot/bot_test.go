// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package bot_test

import (
	"encoding/json"
	"flag"
	"io"
	"log/slog"
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
	"go.astrophena.name/tools/internal/cache"
	"go.astrophena.name/tools/internal/starlark/kvcache"
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

		files := make(map[string]string, len(ar.Files))
		for _, f := range ar.Files {
			files[f.Name] = string(f.Data)
		}

		tm := testMux(t, nil)
		bot := testBot(t, tm, files)

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

func TestLoadReadsItsOwnAssets(t *testing.T) {
	t.Parallel()
	const script = `
def on_load():
    if files.read("asset") != "current":
        fail("wrong asset")
`
	b := testBot(t, testMux(t, nil), map[string]string{
		"bot.star": script,
		"asset":    "current",
	})
	if err := b.Load(t.Context(), map[string]string{
		"bot.star": strings.Replace(script, "current", "updated", 1),
		"asset":    "updated",
	}); err != nil {
		t.Fatal(err)
	}
}

func testBot(t *testing.T, m *mux, files map[string]string) *bot.Bot {
	t.Helper()
	b := bot.New(bot.Opts{
		Token:       tgToken,
		Secret:      "test",
		Owner:       123456789,
		BotID:       987654321,
		BotUsername: "testbot",
		HTTPClient:  testutil.MockHTTPClient(m.mux),
		KVCache:     kvcache.Module(cache.NewMemStore(t.Context(), 1*time.Minute)),
		Logger:      slog.New(slog.NewTextHandler(t.Output(), nil)),
	})
	if err := b.Load(t.Context(), files); err != nil {
		t.Fatal(err)
	}
	return b
}

type mux struct {
	mux           *http.ServeMux
	mu            sync.Mutex
	telegramCalls []call
}

type call struct {
	Method string         `json:"method"`
	Args   map[string]any `json:"args"`
}

const postTelegram = "POST api.telegram.org/{token}/{method}"

func testMux(t *testing.T, overrides map[string]http.HandlerFunc) *mux {
	m := &mux{
		mux: http.NewServeMux(),
	}
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
		if pat == postTelegram {
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

func jsonOK(w http.ResponseWriter) {
	var res struct {
		Status string `json:"status"`
	}
	res.Status = "success"
	web.RespondJSON(w, res)
}

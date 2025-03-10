// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
)

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
		e := testEngine(t, tm)

		_, err = request.Make[any](context.Background(), request.Params{
			Method: http.MethodPost,
			URL:    "/telegram",
			Body:   upd,
			Headers: map[string]string{
				"X-Telegram-Bot-Api-Secret-Token": e.tgSecret,
			},
			HTTPClient: testutil.MockHTTPClient(e.mux),
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

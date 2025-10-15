// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package tgmarkup

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/rr"
	"go.astrophena.name/base/testutil"
)

// Updating this test:
//
//	$  TELEGRAM_TOKEN=... TELEGRAM_CHAT_ID=... go test -httprecord testdata/*.httprr
//
// (notice an extra space before command to prevent recording it in shell
// history)

func TestFromMarkdown(t *testing.T) {
	const (
		expunged       = "EXPUNGED"
		expungedChatID = "-3735928559" // -0xdeadbeef
	)

	testutil.Run(t, "testdata/*.md", func(t *testing.T, match string) {
		recFile := strings.TrimSuffix(match, filepath.Ext(match)) + ".httprr"

		rec, err := rr.Open(recFile, http.DefaultTransport)
		if err != nil {
			t.Fatal(err)
		}
		defer rec.Close()

		token, chatID := expunged, expungedChatID
		if rec.Recording() {
			token = os.Getenv("TELEGRAM_TOKEN")
			chatID = os.Getenv("TELEGRAM_CHAT_ID")
		}

		rec.ScrubReq(func(r *http.Request) error {
			r.URL.Path = strings.ReplaceAll(r.URL.Path, token, expunged)
			return nil
		})
		rec.ScrubResp(func(b *bytes.Buffer) error {
			bu := bytes.ReplaceAll(b.Bytes(), []byte(chatID), []byte(expungedChatID))
			b.Reset()
			b.Write(bu)
			return nil
		})

		b, err := os.ReadFile(match)
		if err != nil {
			t.Fatal(err)
		}

		msg := message{ChatID: chatID}
		msg.Message = FromMarkdown(string(b))

		_, err = request.Make[request.IgnoreResponse](t.Context(), request.Params{
			Method:     http.MethodPost,
			URL:        "https://api.telegram.org/bot" + token + "/sendMessage",
			Body:       msg,
			HTTPClient: rec.Client(),
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

type message struct {
	ChatID string `json:"chat_id"`
	Message
}

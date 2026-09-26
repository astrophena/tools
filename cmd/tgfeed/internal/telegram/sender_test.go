// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package telegram

import (
	"encoding/json"
	"net/http"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/tools/cmd/tgfeed/internal/sender"
)

func TestSend(t *testing.T) {
	t.Parallel()

	var got struct {
		ChatID   string `json:"chat_id"`
		ThreadID int64  `json:"message_thread_id"`
		Text     string `json:"text"`
		Keyboard struct {
			Rows [][]struct {
				Text string `json:"text"`
				URL  string `json:"url"`
			} `json:"inline_keyboard"`
		} `json:"reply_markup"`
	}
	httpc := testutil.MockHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botsecret/sendMessage" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
	}))
	s := New(Config{
		Token:      "secret",
		ChatID:     "default",
		HTTPClient: httpc,
	})

	err := s.Send(t.Context(), sender.Message{
		Body: "**Hello**",
		Target: sender.Target{
			Channel: "override",
			Thread:  "7",
		},
		Actions: []sender.ActionRow{{
			{
				Label: "Open",
				URL:   "https://example.com",
			},
			{
				Label: "invalid",
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ChatID != "override" || got.ThreadID != 7 || got.Text != "Hello\n\n" {
		t.Errorf("request = %+v", got)
	}
	if len(got.Keyboard.Rows) != 1 || len(got.Keyboard.Rows[0]) != 1 {
		t.Errorf("keyboard = %+v", got.Keyboard)
	}
}

func TestSendInvalidThread(t *testing.T) {
	t.Parallel()

	s := New(Config{
		ChatID: "chat",
		Token:  "token",
	})
	err := s.Send(t.Context(), sender.Message{
		Body: "hello",
		Target: sender.Target{
			Thread: "not-a-number",
		},
	})
	if err == nil {
		t.Fatal("Send returned nil error")
	}
}

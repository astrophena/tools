// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package telegram

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

func TestTelegramModule(t *testing.T) {
	cases := map[string]struct {
		script         string
		mockStatusCode int
		mockResponse   string
		wantErr        string
		wantOutput     string
	}{
		"success": {
			script: `
result = telegram.call(
    method = "sendMessage",
    args = {
        "chat_id": 123456789,
        "text": "Hello, world!"
    }
)

print(result)
`,
			mockStatusCode: http.StatusOK,
			mockResponse:   `{"ok":true,"result":{"message_id":123}}`,
			wantOutput:     `{"ok": True, "result": {"message_id": 123}}`,
		},
		"error": {
			script: `
result = telegram.call(
    method = "sendMessage",
    args = {
        "chat_id": 123456789,
        "text": "Hello, world!"
    }
)

print(result)
`,
			mockStatusCode: http.StatusBadRequest,
			mockResponse:   `{"ok": false, "error_code": 400, "description": "Bad Request: chat not found"}`,
			wantErr:        "telegram.call: failed to make request",
		},
		"invalid args": {
			script: `
result = telegram.call(
    "sendMessage",
    {
        "chat_id": 123456789,
        "text": "Hello, world!"
    }
)

print(result)
`,
			wantErr: `telegram.call: unexpected positional arguments`,
		},
		"invalid JSON": {
			script: `
result = telegram.call(
	method="sendMessage", 
	args={"chat_id": 123456789, "text": lambda x: x}
)

print(result)
`,
			wantErr: "telegram.call: failed to encode received args to JSON",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			httpc := testutil.MockHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.mockStatusCode != 0 {
					w.WriteHeader(tc.mockStatusCode)
				}
				if tc.mockResponse != "" {
					w.Write([]byte(tc.mockResponse))
				}
			}))

			var buf bytes.Buffer

			thread := &starlark.Thread{
				Name:  "test",
				Print: func(_ *starlark.Thread, msg string) { fmt.Fprint(&buf, msg) },
			}
			thread.SetLocal("context", context.Background())

			predecl := starlark.StringDict{
				"telegram": Module("123:456", httpc),
			}

			_, err := starlark.ExecFileOptions(&syntax.FileOptions{}, thread, "test.star", tc.script, predecl)

			if tc.wantErr != "" {
				if err == nil {
					t.Errorf("expected error, but got nil")
				} else if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("unexpected error: got %q, want to contain %q", err, tc.wantErr)
				}
			} else {
				if err != nil {
					t.Errorf("failed to execute script: %v", err)
				}
				if tc.wantOutput != "" && buf.String() != tc.wantOutput {
					t.Errorf("unexpected output: got %q, want %q", buf.String(), tc.wantOutput)
				}
			}
		})
	}
}

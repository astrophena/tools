// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package tgstarlark

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"go.starlark.net/starlark"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/tools/internal/starlark/interpreter"
	"go.astrophena.name/tools/internal/starlark/stdlib"
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

			intr := &interpreter.Interpreter{
				Predeclared: starlark.StringDict{
					"telegram": Module("123:456", httpc),
				},
				Packages: map[string]interpreter.Loader{
					interpreter.MainPkg: interpreter.MemoryLoader(map[string]string{
						"test.star": string(tc.script),
					}),
					interpreter.StdlibPkg: stdlib.Loader(),
				},
				Logger: func(file string, line int, message string) {
					fmt.Fprint(&buf, message)
				},
			}
			if err := intr.Init(t.Context()); err != nil {
				t.Fatal(err)
			}

			_, err := intr.ExecModule(t.Context(), interpreter.MainPkg, "test.star")

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

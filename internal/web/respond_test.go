// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/testutil"
)

func TestRespondError(t *testing.T) {
	cases := map[string]struct {
		err        error
		wantStatus int
		wantInBody string
		wantToLog  bool
	}{
		"404": {
			err:        ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantInBody: "404 Not Found",
			wantToLog:  false,
		},
		"500": {
			err:        ErrInternalServerError,
			wantStatus: http.StatusInternalServerError,
			wantInBody: "500 Internal Server Error",
			wantToLog:  true,
		},
		"404 (wrapped)": {
			err:        fmt.Errorf("wrapped: %w", ErrNotFound),
			wantStatus: http.StatusNotFound,
			wantInBody: "404 Not Found",
			wantToLog:  false,
		},
		"500 (wrapped)": {
			err:        fmt.Errorf("wrapped: %w", ErrInternalServerError),
			wantStatus: http.StatusInternalServerError,
			wantInBody: "500 Internal Server Error",
			wantToLog:  true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()

			var stderr bytes.Buffer
			env := &cli.Env{
				Stderr: &stderr,
			}
			ctx := cli.WithEnv(context.Background(), env)

			r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)

			RespondError(w, r, tc.err)

			if tc.wantToLog && stderr.Len() == 0 {
				t.Fatalf("wanted to log a line, but didn't")
			}

			testutil.AssertEqual(t, tc.wantStatus, w.Code)
			if !strings.Contains(w.Body.String(), tc.wantInBody) {
				t.Errorf("want response body to contain %q, got %q", tc.wantInBody, w.Body.String())
			}
		})
	}
}

func TestRespondJSONError(t *testing.T) {
	cases := map[string]struct {
		err        error
		wantStatus int
		wantBody   string
		wantToLog  bool
	}{
		"404": {
			err:        ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantBody: `{
  "status": "error",
  "error": "not found"
}
`,
		},
		"500": {
			err:        ErrInternalServerError,
			wantStatus: http.StatusInternalServerError,
			wantBody: `{
  "status": "error",
  "error": "internal server error"
}
`,
			wantToLog: true,
		},
		"500 (wrapped)": {
			err:        fmt.Errorf("%w: got up on the wrong foot", ErrInternalServerError),
			wantStatus: http.StatusInternalServerError,
			wantBody: `{
  "status": "error",
  "error": "internal server error: got up on the wrong foot"
}
`,
			wantToLog: true,
		},
		"404 (wrapped)": {
			err:        fmt.Errorf("%w: no such key", ErrNotFound),
			wantStatus: http.StatusNotFound,
			wantBody: `{
  "status": "error",
  "error": "not found: no such key"
}
`,
		},
		"500 (default error)": {
			err:        io.EOF,
			wantStatus: http.StatusInternalServerError,
			wantBody: `{
  "status": "error",
  "error": "EOF"
}
`,
			wantToLog: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()

			var stderr bytes.Buffer
			env := &cli.Env{
				Stderr: &stderr,
			}
			ctx := cli.WithEnv(context.Background(), env)

			r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)

			RespondJSONError(w, r, tc.err)

			if tc.wantToLog && stderr.Len() == 0 {
				t.Fatalf("wanted to log a line, but didn't")
			}

			testutil.AssertEqual(t, tc.wantStatus, w.Code)
			testutil.AssertEqual(t, w.Result().Header.Get("Content-Type"), "application/json")
			testutil.AssertEqual(t, w.Body.String(), tc.wantBody)
		})
	}
}

func TestRespondJSON_Valid(t *testing.T) {
	obj := struct {
		Example string `json:"example"`
		Test    bool   `json:"test"`
		FooBar  string `json:"foobar"`
	}{
		Example: "test",
		Test:    false,
		FooBar:  "foobar",
	}

	w := httptest.NewRecorder()
	RespondJSON(w, obj)

	if w.Code != http.StatusOK {
		t.Fatalf("response code is %d, not 200", w.Code)
	}

	want, err := json.MarshalIndent(obj, "", "  ")
	want = append(want, []byte("\n")...)
	if err != nil {
		t.Fatal(err)
	}
	got := w.Body.Bytes()

	testutil.AssertEqual(t, got, want)
}

func TestRespondJSON_Invalid(t *testing.T) {
	// Let's try to marshal a cyclic object, which isn't supported by Go's JSON
	// package. Code stolen from https://stackoverflow.com/q/64437720.
	type Node struct {
		Name string `json:"name"`
		Next *Node  `json:"next"`
	}
	n1 := Node{Name: "111", Next: nil}
	n2 := Node{Name: "222", Next: &n1}
	n3 := Node{Name: "333", Next: &n2}
	n1.Next = &n3

	w := httptest.NewRecorder()
	RespondJSON(w, n1)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("response code is %d, not 500", w.Code)
	}

	const wantErrorText = "JSON marshal error: json: unsupported value: encountered a cycle via *web.Node"

	errResp := testutil.UnmarshalJSON[errorResponse](t, w.Body.Bytes())
	testutil.AssertEqual(t, errResp.Error, wantErrorText)
}

func TestEscapeForJSON(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
	}{
		"empty string":                       {in: "", want: ""},
		"basic string":                       {in: "Hello, world!", want: "Hello, world!"},
		"escape backslash":                   {in: "This has a \\ backslash", want: "This has a \\\\ backslash"},
		"escape quotes":                      {in: "He said, \"Hello\"!", want: "He said, \\\"Hello\\\"!"},
		"escape control character (tab)":     {in: "This has a tab\tcharacter", want: "This has a tab\\\tcharacter"},
		"escape control character (newline)": {in: "This has a newline\ncharacter", want: "This has a newline\\\ncharacter"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := escapeForJSON(tc.in)
			if got != tc.want {
				t.Errorf("escapeForJSON(%q): want %q, got %q", tc.in, tc.want, got)
			}
		})
	}
}

func send(t testing.TB, h http.Handler, method, path string, wantStatus int) string {
	req, err := http.NewRequest(method, path, nil)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if wantStatus != rec.Code {
		t.Fatalf("want response code %d, got %d", wantStatus, rec.Code)
	}

	return rec.Body.String()
}

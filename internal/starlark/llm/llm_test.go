// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package llm

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
	llmapi "go.astrophena.name/tools/internal/api/llm"
	"go.astrophena.name/tools/internal/starlark/interpreter"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func TestContentPartType(t *testing.T) {
	cases := map[string]struct {
		role string
		want string
	}{
		"assistant":    {role: "assistant", want: "output_text"},
		"legacy model": {role: "model", want: "output_text"},
		"user":         {role: "user", want: "input_text"},
		"system":       {role: "system", want: "input_text"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			testutil.AssertEqual(t, contentPartType(tc.role), tc.want)
		})
	}
}

func TestGenerateAndUsage(t *testing.T) {
	var gotParams llmapi.ResponseParams
	httpc := testutil.MockHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Host != "llm.example" || r.URL.Path != "/v1/responses" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want bearer token", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotParams); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"output":[{"type":"message","content":[{"type":"output_text","text":"generated text"}]}],
			"usage":{"input_tokens":12,"output_tokens":3}
		}`))
	}))
	client := &llmapi.Client{
		APIURL:     "https://llm.example/v1",
		APIKey:     "test-key",
		HTTPClient: httpc,
	}
	module := Module(client, filepath.Join(t.TempDir(), "usage.json"))
	thread := new(interpreter.Interpreter).Thread(t.Context())
	contents := starlark.NewList([]starlark.Value{
		starlark.Tuple{starlark.String("user"), starlark.String("hello")},
		starlark.Tuple{starlark.String("assistant"), starlark.String("earlier")},
	})

	value, err := starlark.Call(thread, module.Members["generate"], starlark.Tuple{
		starlark.String("test-model"),
		contents,
		starlark.String("daily"),
	}, []starlark.Tuple{
		{starlark.String("image"), starlark.Bytes("\x89PNG\r\n\x1a\n")},
		{starlark.String("instructions"), starlark.String("be concise")},
	})
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, value, starlark.String("generated text"))
	testutil.AssertEqual(t, gotParams.Model, "test-model")
	testutil.AssertEqual(t, gotParams.Instructions, "be concise")
	if len(gotParams.Input) != 3 {
		t.Fatalf("input messages = %d, want 3", len(gotParams.Input))
	}
	testutil.AssertEqual(t, gotParams.Input[0].Content[0].Type, "input_text")
	testutil.AssertEqual(t, gotParams.Input[1].Content[0].Type, "output_text")
	if got := gotParams.Input[2].Content[0].ImageURL; !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("image URL = %q, want PNG data URL", got)
	}

	usageValue, err := starlark.Call(thread, module.Members["usage"], starlark.Tuple{starlark.String("daily")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	usage := usageValue.(*starlarkstruct.Struct)
	for name, want := range map[string]string{
		"input_tokens":  "12",
		"output_tokens": "3",
		"total_tokens":  "15",
	} {
		got, err := usage.Attr(name)
		if err != nil {
			t.Fatal(err)
		}
		if got.String() != want {
			t.Errorf("usage.%s = %s, want %s", name, got, want)
		}
	}
}

func TestGenerateRejectsInvalidContents(t *testing.T) {
	client := &llmapi.Client{HTTPClient: testutil.MockHTTPClient(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("invalid contents reached the LLM API")
	}))}
	cases := map[string]struct {
		item starlark.Value
		want string
	}{
		"not tuple": {
			item: starlark.String("hello"),
			want: "contents[0] is not a tuple",
		},
		"wrong tuple length": {
			item: starlark.Tuple{starlark.String("user")},
			want: "must have exactly two elements",
		},
		"non-string role": {
			item: starlark.Tuple{starlark.MakeInt(1), starlark.String("hello")},
			want: "role must be a string",
		},
		"non-string content": {
			item: starlark.Tuple{starlark.String("user"), starlark.MakeInt(1)},
			want: "content must be a string",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			module := Module(client, "")
			contents := starlark.NewList([]starlark.Value{tc.item})
			_, err := starlark.Call(new(interpreter.Interpreter).Thread(t.Context()), module.Members["generate"], starlark.Tuple{
				starlark.String("test-model"),
				contents,
				starlark.String(""),
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("generate error = %v, want mention %q", err, tc.want)
			}
		})
	}
}

func TestGenerateWithoutClient(t *testing.T) {
	module := Module(nil, "")
	_, err := starlark.Call(new(interpreter.Interpreter).Thread(t.Context()), module.Members["generate"], nil, nil)
	if err == nil || !strings.Contains(err.Error(), "LLM API is not available") {
		t.Fatalf("generate error = %v, want unavailable error", err)
	}
}

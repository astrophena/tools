// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package llm

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.astrophena.name/base/rr"
	"go.astrophena.name/base/testutil"
)

// Updating this test:
//
//	$  LLM_API_URL=... LLM_API_KEY=... LLM_MODEL=... go test -run TestCreateResponse -httprecord testdata/create_response.httprr
//
// For OpenAI, LLM_API_URL is usually https://api.openai.com/v1. For providers
// such as OpenRouter, use that provider's OpenAI Responses-compatible base URL.
// LLM_MODEL defaults to gpt-4.1-mini.
//
// (notice an extra space before command to prevent recording it in shell
// history)

func TestCreateResponse(t *testing.T) {
	recFile := filepath.Join("testdata", "create_response.httprr")
	recording, err := rr.Recording(recFile)
	if err != nil {
		t.Fatal(err)
	}
	if recording {
		if err := os.MkdirAll(filepath.Dir(recFile), 0o755); err != nil {
			t.Fatal(err)
		}
	} else if _, err := os.Stat(recFile); errors.Is(err, os.ErrNotExist) {
		t.Skipf("%s is not recorded; see update command above", recFile)
	} else if err != nil {
		t.Fatal(err)
	}

	rec, err := rr.Open(recFile, http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	rec.ScrubReq(func(r *http.Request) error {
		r.Header.Del("Authorization")
		r.Host = "llm.example"
		r.URL.Scheme = "https"
		r.URL.Host = "llm.example"
		r.URL.Path = "/responses"
		r.URL.RawQuery = ""
		r.URL.Fragment = ""

		if r.Body == nil {
			return nil
		}
		body := r.Body.(*rr.Body)
		var params ResponseParams
		if err := json.Unmarshal(body.Data, &params); err != nil {
			return err
		}
		params.Model = "test-model"
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		body.Data = data
		body.ReadOffset = 0
		return nil
	})
	rec.ScrubResp(func(b *bytes.Buffer) error {
		resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(b.Bytes())), nil)
		if err != nil {
			return err
		}
		resp.Header.Del("Server")
		resp.Header.Del("Set-Cookie")

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		data = responseUserRe.ReplaceAll(data, []byte(`"user":"example"`))
		resp.Body = io.NopCloser(bytes.NewReader(data))
		resp.ContentLength = int64(len(data))

		b.Reset()
		return resp.Write(b)
	})

	apiURL := "https://llm.example/v1"
	apiKey := "example"
	model := "gpt-4.1-mini"
	if rec.Recording() {
		apiURL = os.Getenv("LLM_API_URL")
		apiKey = os.Getenv("LLM_API_KEY")
		model = cmp.Or(os.Getenv("LLM_MODEL"), model)
		if apiURL == "" {
			t.Fatal("LLM_API_URL must be set when recording")
		}
		if apiKey == "" {
			t.Fatal("LLM_API_KEY must be set when recording")
		}
	}

	c := &Client{
		APIURL:     apiURL,
		APIKey:     apiKey,
		HTTPClient: rec.Client(),
	}

	resp, err := c.CreateResponse(t.Context(), ResponseParams{
		Model: model,
		Input: []Message{{
			Role:    "user",
			Content: []ContentPart{{Type: "input_text", Text: "Reply with one short sentence."}},
		}},
		Instructions: "Be concise.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(resp.OutputText) == "" {
		t.Fatal("response output text is empty")
	}
	if resp.Usage.InputTokens == 0 {
		t.Fatal("response input token usage is empty")
	}
	if resp.Usage.OutputTokens == 0 {
		t.Fatal("response output token usage is empty")
	}
}

var responseUserRe = regexp.MustCompile(`"user":"[^"]*"`)

func TestResponseOutputText(t *testing.T) {
	cases := map[string]struct {
		body string
		want string
	}{
		"output array": {
			body: `{
				"output": [
					{
						"type": "message",
						"content": [
							{"type": "output_text", "text": "hello"},
							{"type": "output_text", "text": " world"}
						]
					}
				],
				"usage": {
					"input_tokens": 12,
					"output_tokens": 3
				}
			}`,
			want: "hello world",
		},
		"output text": {
			body: `{
				"output_text": "hello from gateway",
				"usage": {
					"input_tokens": 12,
					"output_tokens": 3
				}
			}`,
			want: "hello from gateway",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var resp Response
			if err := json.Unmarshal([]byte(tc.body), &resp); err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, resp.OutputText, tc.want)
			testutil.AssertEqual(t, resp.Usage.InputTokens, int64(12))
			testutil.AssertEqual(t, resp.Usage.OutputTokens, int64(3))
		})
	}
}

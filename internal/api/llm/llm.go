// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package llm sends requests to Responses API gateways.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/version"
)

// Client sends requests to a Responses API gateway.
type Client struct {
	// APIURL is the API root URL, for example https://api.openai.com/v1.
	APIURL string
	// APIKey is the API key used for bearer authentication.
	APIKey string
	// HTTPClient defaults to [request.DefaultClient].
	HTTPClient *http.Client
	// Scrubber redacts sensitive text from request errors.
	Scrubber *strings.Replacer
}

// ContentPart is a text or image item in a message.
type ContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// Message is an input message.
type Message struct {
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
}

// ResponseParams defines the request body for POST /responses.
type ResponseParams struct {
	Model        string    `json:"model"`
	Input        []Message `json:"input"`
	Instructions string    `json:"instructions,omitempty"`
}

// Response is a subset of the OpenAI Responses API response.
type Response struct {
	OutputText string `json:"output_text"`
	Usage      struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

type responseOutputItem struct {
	Type    string                  `json:"type"`
	Content []responseOutputContent `json:"content"`
}

type responseOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// UnmarshalJSON decodes the response and derives OutputText from the output
// array when output_text is empty.
func (r *Response) UnmarshalJSON(data []byte) error {
	type response Response
	var resp struct {
		response
		Output []responseOutputItem `json:"output"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return err
	}
	*r = Response(resp.response)
	if r.OutputText == "" {
		r.OutputText = outputText(resp.Output)
	}
	return nil
}

func outputText(items []responseOutputItem) string {
	var b strings.Builder
	for _, output := range items {
		if output.Type != "" && output.Type != "message" {
			continue
		}
		for _, content := range output.Content {
			if content.Type != "" && content.Type != "output_text" {
				continue
			}
			b.WriteString(content.Text)
		}
	}
	return b.String()
}

// CreateResponse sends a POST /responses request.
func (c *Client) CreateResponse(ctx context.Context, params ResponseParams) (*Response, error) {
	if c.APIURL == "" {
		return nil, errors.New("APIURL shouldn't be empty")
	}
	if c.APIKey == "" {
		return nil, errors.New("APIKey shouldn't be empty")
	}
	if params.Model == "" {
		return nil, errors.New("model shouldn't be empty")
	}

	return request.Make[*Response](ctx, request.Params{
		Method: http.MethodPost,
		URL:    strings.TrimRight(c.APIURL, "/") + "/responses",
		Headers: map[string]string{
			"Authorization": "Bearer " + c.APIKey,
			"User-Agent":    version.UserAgent(),
		},
		Body:       params,
		HTTPClient: c.HTTPClient,
		Scrubber:   c.Scrubber,
	})
}

// Package gemini provides a client for interacting with a generative text API.
package gemini

import (
	"context"
	"net/http"

	"go.astrophena.name/tools/internal/request"
)

// APIEndpoint is the base URL for the generative text API.
const APIEndpoint = "https://generativelanguage.googleapis.com/v1beta"

// Client holds configuration for interacting with the generative text API.
type Client struct {
	// APIKey is the API key used for authentication.
	APIKey string
	// Model specifies the name of the model to use for generation.
	Model string
	// HTTPClient is an optional HTTP client to use for requests. Defaults to
	// httputil.DefaultClient.
	HTTPClient *http.Client
}

// GenerateContentParams defines the structure for the request body sent to the
// GenerateContent API.
type GenerateContentParams struct {
	// Contents is a list of Content objects representing the input text for
	// generation.
	Contents []*Content `json:"contents"`
	// SystemInstruction is an optional Content object specifying system
	// instructions for generation.
	SystemInstruction *Content `json:"systemInstruction,omitempty"`
}

// Content represents a piece of text content with a list of Part objects.
type Content struct {
	// Parts is a list of Part objects representing the textual elements within
	// the content.
	Parts []*Part `json:"parts"`
}

// Part represents a textual element within a Content object.
type Part struct {
	// Text is the content of the textual element.
	Text string `json:"text"`
}

// GenerateContentResponse defines the structure of the response received from
// the GenerateContent API.
type GenerateContentResponse struct {
	// Candidates is a list of Candidate objects representing the generated text
	// alternatives.
	Candidates []*Candidate `json:"candidates"`
}

// Candidate represents a generated text candidate with a corresponding Content
// object.
type Candidate struct {
	// Content is the generated text content for this candidate.
	Content *Content `json:"content"`
}

// GenerateContent sends a request to the generative text API to generate
// creative text content.
func (c *Client) GenerateContent(ctx context.Context, params GenerateContentParams) (GenerateContentResponse, error) {
	return request.MakeJSON[GenerateContentResponse](ctx, request.Params{
		Method: http.MethodPost,
		URL:    APIEndpoint + "/models/" + c.Model + ":generateContent",
		Headers: map[string]string{
			"x-goog-api-key": c.APIKey,
		},
		Body:       params,
		HTTPClient: c.HTTPClient,
	})
}

// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package gemini provides a very minimal client for interacting with Gemini
// API.
package gemini

import (
	"context"
	"net/http"
	"strings"

	"go.astrophena.name/base/request"
	"go.astrophena.name/tools/internal/version"
)

const apiURL = "https://generativelanguage.googleapis.com/v1beta"

// Client holds configuration for interacting with the Gemini API.
type Client struct {
	// APIKey is the API key used for authentication.
	APIKey string
	// Model specifies the name of the model to use for generation.
	Model string
	// HTTPClient is an optional HTTP client to use for requests. Defaults to
	// request.DefaultClient.
	HTTPClient *http.Client
	// Scrubber is an optional strings.Replacer that scrubs unwanted data from
	// error messages.
	Scrubber *strings.Replacer
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
	// SafetySettings is a list of unique SafetySetting instances for blocking
	// unsafe content.
	SafetySettings []*SafetySetting `json:"safetySettings,omitempty"`
}

// SafetySetting represents a safety setting, affecting the safety-blocking
// behavior.
type SafetySetting struct {
	Category  HarmCategory       `json:"category"`
	Threshold HarmBlockThreshold `json:"threshold"`
}

// HarmCategory covers various kinds of harms that can be filtered from model
// responses.
type HarmCategory string

// See https://ai.google.dev/api/generate-content#v1beta.HarmCategory for all
// categories.
const (
	DangerousContent HarmCategory = "HARM_CATEGORY_DANGEROUS_CONTENT"
	Harassment       HarmCategory = "HARM_CATEGORY_HARASSMENT"
	HateSpeech       HarmCategory = "HARM_CATEGORY_HATE_SPEECH"
	SexuallyExplicit HarmCategory = "HARM_CATEGORY_SEXUALLY_EXPLICIT"
)

// HarmBlockThreshold represents a threshold to block at and beyond a specified
// harm probability.
type HarmBlockThreshold string

// See https://ai.google.dev/api/generate-content#harmblockthreshold for all
// thresholds.
const (
	BlockNone HarmBlockThreshold = "BLOCK_NONE"
)

// Content represents a piece of text content with a list of Part objects.
type Content struct {
	// Parts is a list of Part objects representing the textual elements within
	// the content.
	Parts []*Part `json:"parts"`
	// Role is the producer of the content. Must be either 'user' or 'model'.
	Role string `json:"role,omitempty"`
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

// GenerateContent sends a request to the Gemini API to generate creative text
// content.
func (c *Client) GenerateContent(ctx context.Context, params GenerateContentParams) (GenerateContentResponse, error) {
	return request.Make[GenerateContentResponse](ctx, request.Params{
		Method: http.MethodPost,
		URL:    apiURL + "/models/" + c.Model + ":generateContent",
		Headers: map[string]string{
			"x-goog-api-key": c.APIKey,
			"User-Agent":     version.UserAgent(),
		},
		Body:       params,
		HTTPClient: c.HTTPClient,
		Scrubber:   c.Scrubber,
	})
}

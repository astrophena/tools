// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package gist provides a GitHub Gist client.
package gist

import (
	"context"
	"net/http"
	"strings"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/version"
)

const apiURL = "https://api.github.com/gists/"

// Client retrieves and updates GitHub Gists.
type Client struct {
	// Token is the GitHub access token.
	Token string
	// HTTPClient defaults to [request.DefaultClient].
	HTTPClient *http.Client
	// Scrubber redacts sensitive text from request errors.
	Scrubber *strings.Replacer
}

func (c *Client) makeRequest(ctx context.Context, method, id string, gist *Gist) (*Gist, error) {
	rp := request.Params{
		Method: method,
		URL:    apiURL + id,
		Headers: map[string]string{
			"Accept":               "application/vnd.github+json",
			"X-GitHub-Api-Version": "2022-11-28",
			"User-Agent":           version.UserAgent(),
		},
		HTTPClient: c.HTTPClient,
		Scrubber:   c.Scrubber,
	}
	if gist != nil {
		rp.Body = gist
	}
	if c.Token != "" {
		rp.Headers["Authorization"] = "Bearer " + c.Token
	}
	return request.Make[*Gist](ctx, rp)
}

// Get retrieves a [Gist].
func (c *Client) Get(ctx context.Context, id string) (*Gist, error) {
	return c.makeRequest(ctx, http.MethodGet, id, nil)
}

// Update modifies an existing [Gist].
func (c *Client) Update(ctx context.Context, id string, gist *Gist) (*Gist, error) {
	return c.makeRequest(ctx, http.MethodPatch, id, gist)
}

// Gist represents a GitHub Gist.
type Gist struct {
	Files map[string]File `json:"files"`
}

// File represents a file within a [Gist].
type File struct {
	Content string `json:"content"`
}

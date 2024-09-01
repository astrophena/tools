// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package gist provides a very minimal client for interacting with the GitHub
// Gist API.
//
// To use this package, you need to create a [Client] object with your access
// token. Then, you can use the [Client.Get] or [Client.Update] methods to
// retrieve or modify Gists.
package gist

import (
	"context"
	"net/http"
	"strings"

	"go.astrophena.name/base/request"
	"go.astrophena.name/tools/internal/version"
)

const apiURL = "https://api.github.com"

// Client represents a GitHub Gist API client.
type Client struct {
	// Token is the GitHub access token used for authentication.
	Token string
	// HTTPClient is an optional custom HTTP client object to use for requests.
	// If not provided, request.DefaultClient will be used.
	HTTPClient *http.Client
	// Scrubber is an optional strings.Replacer that scrubs unwanted data from
	// error messages.
	Scrubber *strings.Replacer
}

// makeRequest performs a generic HTTP request to the GitHub Gist API using the
// provided parameters.
func (c *Client) makeRequest(ctx context.Context, method string, id string, gist *Gist) (*Gist, error) {
	rp := request.Params{
		Method: method,
		URL:    apiURL + "/gists/" + id,
		Headers: map[string]string{
			"Accept":               "application/vnd.github+json",
			"X-GitHub-Api-Version": "2022-11-28",
			"User-Agent":           version.UserAgent(),
		},
		Body:       gist,
		HTTPClient: c.HTTPClient,
		Scrubber:   c.Scrubber,
	}
	if c.Token != "" {
		rp.Headers["Authorization"] = "Bearer " + c.Token
	}
	return request.Make[*Gist](ctx, rp)
}

// Get retrieves a Gist with the specified ID from GitHub.
func (c *Client) Get(ctx context.Context, id string) (*Gist, error) {
	return c.makeRequest(ctx, http.MethodGet, id, nil)
}

// Update modifies an existing Gist with the specified ID on GitHub.
func (c *Client) Update(ctx context.Context, id string, gist *Gist) (*Gist, error) {
	return c.makeRequest(ctx, http.MethodPatch, id, gist)
}

// Gist represents a GitHub Gist data structure.
type Gist struct {
	// Files is a map containing file names as keys and their corresponding File
	// data as values.
	Files map[string]File `json:"files"`
}

// File represents a file within a Gist.
type File struct {
	// Content is the textual content of the file.
	Content string `json:"content"`
}

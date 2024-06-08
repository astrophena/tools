// Package httputil provides utilities for making HTTP requests.
package httputil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.astrophena.name/tools/internal/version"
)

// RequestParams defines the parameters needed for making an HTTP request.
type RequestParams struct {
	// Method is the HTTP method (GET, POST, etc.) for the request.
	Method string
	// URL is the target URL of the request.
	URL string
	// Headers is a map of key-value pairs for additional request headers.
	Headers map[string]string
	// Body is any data to be sent in the request body. It will be marshaled to
	// JSON.
	Body any
	// HTTPClient is an optional custom HTTP client object to use for the request.
	// If not provided, http.DefaultClient will be used.
	HTTPClient *http.Client
}

// MakeRequest makes a generic HTTP request with the provided parameters and
// unmarshals the JSON response body into the specified type.
func MakeRequest[R any](ctx context.Context, params RequestParams) (R, error) {
	var resp R

	var data []byte
	if params.Body != nil {
		var err error
		data, err = json.Marshal(params.Body)
		if err != nil {
			return resp, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, params.Method, params.URL, bytes.NewReader(data))
	if err != nil {
		return resp, err
	}

	if params.Headers != nil {
		for k, v := range params.Headers {
			req.Header.Set(k, v)
		}
	}
	req.Header.Set("User-Agent", UserAgent())
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	httpc := http.DefaultClient
	if params.HTTPClient != nil {
		httpc = params.HTTPClient
	}

	res, err := httpc.Do(req)
	if err != nil {
		return resp, err
	}
	defer res.Body.Close()

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return resp, err
	}

	if res.StatusCode != http.StatusOK {
		return resp, fmt.Errorf("%s %q: want 200, got %d: %s", params.Method, params.URL, res.StatusCode, b)
	}

	if err := json.Unmarshal(b, &resp); err != nil {
		return resp, err
	}

	return resp, nil
}

// UserAgent returns a user agent string by combining the version information
// and a special URL leading to bot information page.
func UserAgent() string {
	return strings.Replace(version.Version().Short(), " ", "/", 1) + " (+https://astrophena.name/bleep-bloop)"
}

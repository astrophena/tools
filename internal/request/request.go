// Package request provides utilities for making HTTP requests.
package request

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.astrophena.name/tools/internal/version"
)

// DefaultClient is a [http.Client] with nice defaults.
var DefaultClient = &http.Client{
	Timeout: 10 * time.Second,
}

// Params defines the parameters needed for making an HTTP request.
type Params struct {
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
	// If not provided, [DefaultClient] will be used.
	HTTPClient *http.Client
}

// MakeJSON makes a JSON HTTP request with the provided parameters and
// unmarshals the JSON response body into the specified type.
func MakeJSON[Response any](ctx context.Context, p Params) (Response, error) {
	var resp Response

	var data []byte
	if p.Body != nil {
		var err error
		data, err = json.Marshal(p.Body)
		if err != nil {
			return resp, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, p.Method, p.URL, bytes.NewReader(data))
	if err != nil {
		return resp, err
	}

	if p.Headers != nil {
		for k, v := range p.Headers {
			req.Header.Set(k, v)
		}
	}
	req.Header.Set("User-Agent", UserAgent())
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	httpc := DefaultClient
	if p.HTTPClient != nil {
		httpc = p.HTTPClient
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
		return resp, fmt.Errorf("%s %q: want 200, got %d: %s", p.Method, p.URL, res.StatusCode, b)
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

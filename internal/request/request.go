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
	// If not provided, DefaultClient will be used.
	HTTPClient *http.Client
	// Scrubber is an optional strings.Replacer that scrubs unwanted data from
	// error messages.
	Scrubber *strings.Replacer
}

type scrubbedError struct {
	err      error
	scrubber *strings.Replacer
}

func (se *scrubbedError) Error() string {
	if se.scrubber != nil {
		return se.scrubber.Replace(se.err.Error())
	}
	return se.err.Error()
}

func (se *scrubbedError) Unwrap() error { return se.err }

func scrubErr(err error, scrubber *strings.Replacer) error {
	return &scrubbedError{err: err, scrubber: scrubber}
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
			return resp, scrubErr(err, p.Scrubber)
		}
	}

	var br io.Reader
	if data != nil {
		br = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, p.Method, p.URL, br)
	if err != nil {
		return resp, scrubErr(err, p.Scrubber)
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
		return resp, scrubErr(err, p.Scrubber)
	}
	defer res.Body.Close()

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return resp, scrubErr(err, p.Scrubber)
	}

	if res.StatusCode != http.StatusOK {
		return resp, scrubErr(fmt.Errorf("%s %q: want 200, got %d: %s", p.Method, p.URL, res.StatusCode, b), p.Scrubber)
	}

	if err := json.Unmarshal(b, &resp); err != nil {
		return resp, scrubErr(err, p.Scrubber)
	}

	return resp, nil
}

// UserAgent returns a user agent string by combining the version information
// and a special URL leading to bot information page.
func UserAgent() string {
	i := version.Version()
	ver := i.Version
	if i.Version == "devel" && i.Commit != "" {
		ver = i.Commit
	}
	return i.Name + "/" + ver + " (+https://astrophena.name/bleep-bloop)"
}

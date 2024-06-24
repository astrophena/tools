// Package httputil provides utilities for making and serving HTTP requests.
package httputil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"go.astrophena.name/tools/internal/version"
)

// DefaultClient is a [http.Client] with nice defaults.
var DefaultClient = &http.Client{
	Timeout: 10 * time.Second,
}

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
	// If not provided, [DefaultClient] will be used.
	HTTPClient *http.Client
}

func dumpJSON() bool { return os.Getenv("DUMP_JSON") == "1" }

// MakeJSONRequest makes a JSON HTTP request with the provided parameters and
// unmarshals the JSON response body into the specified type.
func MakeJSONRequest[R any](ctx context.Context, params RequestParams) (R, error) {
	var resp R

	var data []byte
	if params.Body != nil {
		var err error
		data, err = json.Marshal(params.Body)
		if err != nil {
			return resp, err
		}
		if dumpJSON() {
			log.Printf("httputil: %s %s: sent JSON: %v", params.Method, params.URL, string(data))
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

	httpc := DefaultClient
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
	if dumpJSON() {
		log.Printf("httputil: %s %q: received JSON: %s", params.Method, params.URL, b)
	}

	return resp, nil
}

// UserAgent returns a user agent string by combining the version information
// and a special URL leading to bot information page.
func UserAgent() string {
	return strings.Replace(version.Version().Short(), " ", "/", 1) + " (+https://astrophena.name/bleep-bloop)"
}

// StatusErr is a sentinel error type used to represent HTTP status code errors.
// It wraps the corresponding http.Status code.
type StatusErr int

// Error implements the error interface.
// It returns a lowercase representation of the HTTP status text for the wrapped code.
func (se StatusErr) Error() string {
	return strings.ToLower(http.StatusText(int(se)))
}

const (
	// ErrUnauthorized represents an unauthorized access error (HTTP 401).
	ErrUnauthorized StatusErr = http.StatusUnauthorized
	// ErrForbidden represents a forbidden access error (HTTP 403).
	ErrForbidden StatusErr = http.StatusForbidden
	// ErrNotFound represents a not found error (HTTP 404).
	ErrNotFound StatusErr = http.StatusNotFound
	// ErrMethodNotAllowed represents a method not allowed error (HTTP 405).
	ErrMethodNotAllowed StatusErr = http.StatusMethodNotAllowed
	// ErrBadRequest represents a bad request error (HTTP 400).
	ErrBadRequest StatusErr = http.StatusBadRequest
)

// errorResponse is a struct used to represent an error response in JSON format.
type errorResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

// RespondJSON marshals the provided response object as JSON and writes it to the http.ResponseWriter.
// It sets the Content-Type header to application/json before marshalling.
// In case of marshalling errors, it writes an internal server error with the error message.
func RespondJSON(w http.ResponseWriter, response any) {
	w.Header().Set("Content-Type", "application/json")
	b, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf(`{
      "status": "error",
      "error": "JSON marshal error: %v"
    }
`, err)))
		return
	}
	w.Write(b)
	w.Write([]byte("\n"))
}

// RespondJSONError writes an error response in JSON format to w.
//
// If the error is a StatusErr, it extracts the HTTP status code and sets the
// response status code accordingly. Otherwise, it sets the response status code
// to http.StatusInternalServerError.
//
// You can wrap any error with fmt.Errorf to create a StatusErr and set a
// specific HTTP status code:
//
//	// This will set the status code to 404 (Not Found).
//	httputil.RespondJSONError(w, fmt.Errorf("resource %w", httputil.ErrNotFound)
func RespondJSONError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	var se StatusErr
	if errors.As(err, &se) {
		w.WriteHeader(int(se))
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
	RespondJSON(w, &errorResponse{Status: "error", Error: err.Error()})
}

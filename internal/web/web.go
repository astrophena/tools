// Package web is a collection of functions and types for building web services.
package web

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"go.astrophena.name/tools/internal/logger"
)

// StatusErr is a sentinel error type used to represent HTTP status code errors.
type StatusErr int

// Error implements the error interface.
// It returns a lowercase representation of the HTTP status text for the wrapped code.
func (se StatusErr) Error() string { return strings.ToLower(http.StatusText(int(se))) }

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
	// ErrInternalServerError represents an internal server error (HTTP 500).
	ErrInternalServerError StatusErr = http.StatusInternalServerError
)

// errorResponse is a struct used to represent an error response in JSON format.
type errorResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

// RespondJSON marshals the provided response object as JSON and writes it to the http.ResponseWriter.
// It sets the Content-Type header to application/json before marshalling.
// In case of marshalling errors, it writes an internal server error with the error message.
func RespondJSON(w http.ResponseWriter, response any) { respondJSON(w, response, false) }

func respondJSON(w http.ResponseWriter, response any, wroteStatus bool) {
	w.Header().Set("Content-Type", "application/json")
	b, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		if !wroteStatus {
			w.WriteHeader(http.StatusInternalServerError)
		}
		w.Write([]byte(fmt.Sprintf(`{
  "status": "error",
  "error": "JSON marshal error: %s"
}`, escapeForJSON(err.Error()))))
		return
	}
	w.Write(b)
	w.Write([]byte("\n"))
}

//go:embed templates/error.html
var errorTemplate string

// RespondError writes an error response in HTML format to w and logs the error
// using logf if error is [ErrInternalServerError].
//
// If the error is a StatusErr or wraps it, it extracts the HTTP status code and
// sets the response status code accordingly. Otherwise, it sets the response
// status code to http.StatusInternalServerError.
//
// You can wrap any error with fmt.Errorf to create a StatusErr and set a
// specific HTTP status code:
//
//	// This will set the status code to 404 (Not Found).
//	web.RespondError(w, web.ErrNotFound)
func RespondError(logf logger.Logf, w http.ResponseWriter, err error) {
	respondError(false, logf, w, err)
}

// RespondJSONError writes an error response in JSON format to w and logs the
// error using logf.
//
// If the error is a StatusErr or wraps it, it extracts the HTTP status code and sets the
// response status code accordingly. Otherwise, it sets the response status code
// to http.StatusInternalServerError.
//
// You can wrap any error with fmt.Errorf to create a StatusErr and set a
// specific HTTP status code:
//
//	// This will set the status code to 404 (Not Found).
//	web.RespondJSONError(w, fmt.Errorf("resource %w", web.ErrNotFound)
func RespondJSONError(logf logger.Logf, w http.ResponseWriter, err error) {
	respondError(true, logf, w, err)
}

func respondError(json bool, logf logger.Logf, w http.ResponseWriter, err error) {
	var se StatusErr
	if !errors.As(err, &se) {
		se = ErrInternalServerError
	}
	w.WriteHeader(int(se))
	if se == ErrInternalServerError {
		logf("Error %d (%s): %v", se, http.StatusText(int(se)), err)
	}
	if json {
		respondJSON(w, &errorResponse{Status: "error", Error: err.Error()}, true)
		return
	}
	fmt.Fprintf(w, errorTemplate, int(se), http.StatusText(int(se)))
}

func escapeForJSON(s string) string {
	var sb strings.Builder
	for _, ch := range s {
		switch ch {
		case '\\', '"', '/', '\b', '\n', '\r', '\t':
			// Escape these characters with a backslash.
			sb.WriteRune('\\')
			sb.WriteRune(ch)
		default:
			sb.WriteRune(ch)
		}
	}
	return sb.String()
}

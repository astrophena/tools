package web

import (
	_ "embed"
	"fmt"
	"log"
	"net/http"
)

var (
	//go:embed bad_request.html
	badRequestTmpl string

	//go:embed error.html
	errorTmpl string

	//go:embed not_found.html
	notFoundTmpl string
)

var logf = log.Printf // used in tests

// BadRequest replies to the request with an HTTP 400 bad request error.
func BadRequest(w http.ResponseWriter, r *http.Request, message string) {
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, badRequestTmpl, message)
}

// Error replies to the request with an HTTP 500 internal server error and logs
// the error.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprintf(w, errorTmpl, err)
	logf("HTTP error: %v", err)
}

// NotFound replies to the request with an HTTP 404 not found error.
func NotFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, notFoundTmpl)
}
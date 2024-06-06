package web

import (
	"errors"
	"net/http"
	"testing"
)

func TestErrors(t *testing.T) {
	cases := map[string]struct {
		h          http.Handler
		wantStatus int
	}{
		"BadRequest": {
			h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				BadRequest(w, r, "bad request")
			}),
			wantStatus: http.StatusBadRequest,
		},
		"Error": {
			h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Error(w, r, errors.New("something went wrong"))
			}),
			wantStatus: http.StatusInternalServerError,
		},
		"NotFound": {
			h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				NotFound(w, r)
			}),
			wantStatus: http.StatusNotFound,
		},
	}

	logf = func(format string, args ...any) {}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			send(t, tc.h, http.MethodGet, "/", tc.wantStatus)
		})
	}
}

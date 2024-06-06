package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func send(t testing.TB, h http.Handler, method, path string, wantStatus int) string {
	req, err := http.NewRequest(method, path, nil)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if wantStatus != rec.Code {
		t.Fatalf("want response code %d, got %d", wantStatus, rec.Code)
	}

	return rec.Body.String()
}

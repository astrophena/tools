package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args               []string
		wantErr            error
		wantNothingPrinted bool
		wantInStdout       string
		wantInStderr       string
	}{
		"version flag": {
			args:         []string{"-version"},
			wantInStderr: "starbuck",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			s := new(server)
			err := s.run(context.Background(), tc.args, &stdout, &stderr)

			// Don't use && because we want to trap all cases where err is
			// nil.
			if err == nil {
				if tc.wantErr != nil {
					t.Fatalf("must fail with error: %v", tc.wantErr)
				}
			}

			if err != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("got error: %v", err)
			}

			if tc.wantNothingPrinted {
				if stdout.String() != "" {
					t.Errorf("stdout must be empty, got: %q", stdout.String())
				}
				if stderr.String() != "" {
					t.Errorf("stderr must be empty, got: %q", stderr.String())
				}
			}

			if tc.wantInStdout != "" && !strings.Contains(stdout.String(), tc.wantInStdout) {
				t.Errorf("stdout must contain %q, got: %q", tc.wantInStdout, stdout.String())
			}
			if tc.wantInStderr != "" && !strings.Contains(stderr.String(), tc.wantInStderr) {
				t.Errorf("stderr must contain %q, got: %q", tc.wantInStderr, stderr.String())
			}
		})
	}
}

func TestServer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		method     string
		path       string
		wantStatus int
	}{
		{http.MethodGet, "/reqinfo", http.StatusOK},
		{http.MethodPost, "/reqinfo", http.StatusNotFound},
		{http.MethodGet, "/sha", http.StatusOK},
		{http.MethodPost, "/sha", http.StatusNotFound},
		{http.MethodGet, "/foo", http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			s := new(server)
			s.init.Do(s.doInit)
			send(t, s, tc.method, tc.path, tc.wantStatus)
		})
	}
}

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

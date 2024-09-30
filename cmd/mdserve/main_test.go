// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestEngineMain(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args               []string
		wantErr            error
		wantNothingPrinted bool
		wantInStdout       string
		wantInStderr       string
	}{
		"prints usage with help flag": {
			args:         []string{"-h"},
			wantErr:      flag.ErrHelp,
			wantInStderr: "Usage: mdserve",
		},
		"version": {
			args: []string{"-version"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var (
				e              = new(engine)
				stdout, stderr bytes.Buffer
			)

			err := e.main(context.Background(), tc.args, &stdout, &stderr)

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

func TestServe(t *testing.T) {
	cases := map[string]struct {
		files      map[string]string
		path       string
		wantStatus int
		wantInBody string
	}{
		"not found": {
			path:       "/404.md",
			wantStatus: http.StatusNotFound,
			wantInBody: "404 Not Found",
		},
		"without title and infers index.md": {
			files: map[string]string{
				"index.md": "Hello, world!",
			},
			path:       "/",
			wantStatus: http.StatusOK,
			wantInBody: "<p>Hello, world!</p>",
		},
		"correctly parses title": {
			files: map[string]string{
				"index.md": `# Hello, world!

This is bla bla bla.
`,
			},
			path:       "/index.md",
			wantStatus: http.StatusOK,
			wantInBody: "<title>Hello, world!</title>",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := &engine{
				fs: filesToFS(tc.files),
			}

			r := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			e.ServeHTTP(w, r)

			if w.Code != tc.wantStatus {
				t.Errorf("want status %d, got %d", tc.wantStatus, w.Code)
			}

			if tc.wantInBody != "" && !strings.Contains(w.Body.String(), tc.wantInBody) {
				t.Errorf("body must contain %q, got %q", tc.wantInBody, w.Body.String())
			}
		})
	}
}

func filesToFS(files map[string]string) fs.FS {
	fs := make(fstest.MapFS)
	for name, content := range files {
		fs[name] = &fstest.MapFile{
			Data: []byte(content),
		}
	}
	return fs
}

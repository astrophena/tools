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
	"os"
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
		checkFunc          func(t *testing.T, e *engine)
	}{
		"prints usage with help flag": {
			args:         []string{"-h"},
			wantErr:      flag.ErrHelp,
			wantInStderr: "Usage: mdserve",
		},
		"version": {
			args: []string{"-version"},
		},
		"serves in current dir when passed no args": {
			args:         []string{},
			wantInStderr: "Serving from [CURDIR].",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var (
				e              = new(engine)
				stdout, stderr bytes.Buffer
			)

			e.noServerStart = true
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
			if strings.Contains(tc.wantInStderr, "[CURDIR]") {
				wd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				tc.wantInStderr = strings.ReplaceAll(tc.wantInStderr, "[CURDIR]", wd)
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
		failRead   bool
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
		"serves static file": {
			files: map[string]string{
				"hello.js": `alert("Hello, world!");`,
			},
			path:       "/hello.js",
			wantStatus: http.StatusOK,
			wantInBody: `alert("Hello, world!");`,
		},
		"returns 404 when requesting directory": {
			files: map[string]string{
				"hello/world": "foobar",
			},
			path:       "/hello/",
			wantStatus: http.StatusNotFound,
			wantInBody: "404 Not Found",
		},
		"correctly handles files in directory": {
			files: map[string]string{
				"hello/world": "foobar",
			},
			path:       "/hello/world",
			wantStatus: http.StatusOK,
			wantInBody: "foobar",
		},
		"returns 500 when fails to read": {
			files:      map[string]string{},
			path:       "/hello",
			wantStatus: http.StatusInternalServerError,
			wantInBody: "500 Internal Server Error",
			failRead:   true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := &engine{
				fs: filesToFS(tc.files),
			}
			if tc.failRead {
				e.fs = &failFS{}
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

type failFS struct{}

func (*failFS) Open(name string) (fs.File, error) { return nil, errors.New("failed") }

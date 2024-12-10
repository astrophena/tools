// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"errors"
	"flag"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"go.astrophena.name/base/logger"
	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/cli/clitest"
)

func TestEngineMain(t *testing.T) {
	t.Parallel()

	clitest.Run(t, func(t *testing.T) *engine {
		e := new(engine)
		e.noServerStart = true
		return e
	}, map[string]clitest.Case[*engine]{
		"prints usage with help flag": {
			Args:    []string{"-h"},
			WantErr: flag.ErrHelp,
		},
		"version": {
			Args:    []string{"-version"},
			WantErr: cli.ErrExitVersion,
		},
		"serves in current dir when passed no args": {
			Args: []string{},
		},
	})
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

			env := &cli.Env{
				Stderr: logger.Logf(t.Logf),
			}

			r := httptest.NewRequestWithContext(cli.WithEnv(context.Background(), env), http.MethodGet, tc.path, nil)
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

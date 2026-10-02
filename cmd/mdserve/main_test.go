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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/cli/clitest"
	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
)

var update = flag.Bool("update", false, "update golden files in testdata")

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

func setupTestContext(t *testing.T) (context.Context, *bytes.Buffer) {
	var buf bytes.Buffer
	level := new(slog.LevelVar)
	level.Set(slog.LevelDebug)
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: level})
	l := &logger.Logger{
		Logger: slog.New(h),
		Level:  level,
	}
	ctx := logger.Put(t.Context(), l)
	return ctx, &buf
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
		"infers README.md when index.md does not exist": {
			files: map[string]string{
				"README.md": "Hello, world!",
			},
			path:       "/",
			wantStatus: http.StatusOK,
			wantInBody: "<p>Hello, world!</p>",
		},
		"infers index.md when both index.md and README.md does exist": {
			files: map[string]string{
				"README.md": "Hello, world from README.md!",
				"index.md":  "Hello, world from index.md!",
			},
			path:       "/",
			wantStatus: http.StatusOK,
			wantInBody: "<p>Hello, world from index.md!</p>",
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
		"renders image": {
			files: map[string]string{
				"index.md": "![A beautiful landscape](sample.jpg)",
			},
			path:       "/",
			wantStatus: http.StatusOK,
			wantInBody: `<figure>
<img src="sample.jpg" alt="A beautiful landscape" title="">
<figcaption>A beautiful landscape</figcaption>
</figure>`,
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

			ctx, logBuf := setupTestContext(t)
			r := httptest.NewRequestWithContext(ctx, http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			e.ServeHTTP(w, r)

			if w.Code != tc.wantStatus {
				t.Errorf("want status %d, got %d", tc.wantStatus, w.Code)
			}

			if tc.wantInBody != "" && !strings.Contains(w.Body.String(), tc.wantInBody) {
				t.Errorf("body must contain %q, got %q", tc.wantInBody, w.Body.String())
			}

			if tc.failRead {
				if logBuf.Len() == 0 {
					t.Error("expected an error to be logged, but log buffer is empty")
				}
				if !strings.Contains(logBuf.String(), `"level":"ERROR"`) {
					t.Errorf("expected ERROR log, but got: %s", logBuf.String())
				}
			}
		})
	}
}

func TestPage(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, match string) []byte {
		ar, err := txtar.ParseFile(match)
		if err != nil {
			t.Fatal(err)
		}
		files := make(fstest.MapFS)
		for _, f := range ar.Files {
			files[f.Name] = &fstest.MapFile{Data: f.Data}
		}
		e := &engine{fs: files}
		ctx, _ := setupTestContext(t)
		r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		testutil.AssertEqual(t, w.Code, http.StatusOK)
		testutil.AssertEqual(t, w.Header().Get("Content-Type"), "text/html; charset=utf-8")

		// Asset hashes depend on CSS and JS, not on the page being tested.
		body := w.Body.String()
		for _, name := range []string{"static/css/app.css", "static/js/app.js"} {
			body = strings.ReplaceAll(body, e.srv.StaticHashName(name), name)
		}
		return []byte(body)
	}, *update)
}

func TestStaticAssets(t *testing.T) {
	cases := map[string]string{
		"stylesheet": "static/css/app.css",
		"script":     "static/js/app.js",
	}
	for name, file := range cases {
		t.Run(name, func(t *testing.T) {
			e := &engine{fs: filesToFS(map[string]string{"index.md": "# Hello"})}
			e.init.Do(e.doInit)
			ctx, _ := setupTestContext(t)
			asset := e.srv.StaticHashName(file)
			w := httptest.NewRecorder()
			e.srv.ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil))
			if !strings.Contains(w.Body.String(), `"/`+asset+`"`) {
				t.Fatalf("page does not reference hashed asset %q", asset)
			}

			w = httptest.NewRecorder()
			e.srv.ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodGet, "/"+asset, nil))
			testutil.AssertEqual(t, w.Code, http.StatusOK)
			want, err := staticFS.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, w.Body.Bytes(), want)
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

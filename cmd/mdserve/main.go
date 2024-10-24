// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Mdserve serves Markdown files from a directory.
package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"go.astrophena.name/base/logger"
	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/web"

	"rsc.io/markdown"
)

//go:embed template.html
var tmpl string

func main() {
	cli.Run(func(ctx context.Context) error {
		return new(engine).main(ctx, os.Args[1:], os.Stdout, os.Stderr)
	})
}

type engine struct {
	init sync.Once
	md   *markdown.Parser

	// configuration
	fs   fs.FS
	logf logger.Logf

	// used in tests
	noServerStart bool
}

func (e *engine) main(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	// Define and parse flags.
	a := &cli.App{
		Name:        "mdserve",
		ArgsUsage:   "[flags...] [dir]",
		Description: helpDoc,
		Credits:     credits,
		Flags:       flag.NewFlagSet("mdserve", flag.ContinueOnError),
	}
	var (
		addr = a.Flags.String("addr", "localhost:3000", "Listen on `host:port`.")
	)
	if err := a.HandleStartup(args, stdout, stderr); err != nil {
		if errors.Is(err, cli.ErrExitVersion) {
			return nil
		}
		return err
	}

	var dir string
	if len(a.Flags.Args()) == 1 {
		dir = a.Flags.Args()[0]
	}
	if realdir, err := filepath.Abs(dir); err == nil {
		dir = realdir
	}
	if e.fs == nil && dir != "" {
		e.fs = os.DirFS(dir)
	}

	e.logf = log.New(stderr, "", 0).Printf

	e.init.Do(e.doInit)

	mux := http.NewServeMux()
	mux.Handle("/", e)

	e.logf("Serving from %s.", e.fs)

	if e.noServerStart {
		return nil
	}

	return web.ListenAndServe(ctx, &web.ListenAndServeConfig{
		Addr: *addr,
		Logf: e.logf,
		Mux:  mux,
	})
}

func (e *engine) doInit() {
	// Serve by default from current directory.
	if e.fs == nil {
		e.fs = os.DirFS(".")
	}
	// No logger passed? Throw all logs away.
	if e.logf == nil {
		e.logf = func(format string, args ...any) {}
	}
	e.md = &markdown.Parser{
		Table: true,
	}
}

func (e *engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.init.Do(e.doInit)

	p := r.URL.Path
	if p == "/" {
		p = "/index.md"
	}
	p = strings.TrimPrefix(path.Clean(p), "/")

	fi, err := fs.Stat(e.fs, p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			e.respondError(w, web.ErrNotFound)
			return
		}
		e.respondError(w, fmt.Errorf("reading file info: %w", err))
		return
	}
	if fi.IsDir() {
		e.respondError(w, web.ErrNotFound)
		return
	}

	b, err := fs.ReadFile(e.fs, p)
	if err != nil {
		e.respondError(w, fmt.Errorf("reading file: %w", err))
		return
	}

	if !strings.HasSuffix(p, ".md") {
		http.ServeContent(w, r, fi.Name(), fi.ModTime(), bytes.NewReader(b))
		return
	}

	doc := e.md.Parse(string(b))
	title := parseTitle(b)

	fmt.Fprintf(w, tmpl, title, markdown.ToHTML(doc), web.StaticFS.HashName("static/css/main.css"))
}

func parseTitle(b []byte) string {
	var title string
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		if strings.HasPrefix(s.Text(), "#") {
			title = strings.TrimPrefix(s.Text(), "# ")
			break
		}
	}
	if title == "" {
		return ""
	}
	return title
}

func (e *engine) respondError(w http.ResponseWriter, err error) {
	web.RespondError(e.logf, w, err)
}

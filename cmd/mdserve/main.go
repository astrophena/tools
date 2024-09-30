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
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"go.astrophena.name/base/logger"
	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/web"

	"rsc.io/markdown"
)

var (
	//go:embed template.html
	tmplHTML string
	tmpl     = template.Must(template.New("").Parse(tmplHTML))
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	e := new(engine)
	cli.Run(e.main(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

type engine struct {
	init sync.Once

	fs   fs.FS
	logf logger.Logf

	md *markdown.Parser
}

func (e *engine) main(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	// Define and parse flags.
	a := &cli.App{
		Name:        "mdserve",
		ArgsUsage:   "[flags...] [dir]",
		Description: helpDoc,
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

	if !strings.HasSuffix(p, ".md") {
		e.respondError(w, web.ErrNotFound)
		return
	}

	b, err := fs.ReadFile(e.fs, p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			e.respondError(w, web.ErrNotFound)
			return
		}
		e.respondError(w, fmt.Errorf("reading file: %w", err))
		return
	}

	doc := e.md.Parse(string(b))
	title, err := parseTitle(b)
	if err != nil {
		e.respondError(w, fmt.Errorf("parsing title: %w", err))
	}

	data := struct {
		Title      string
		Content    template.HTML
		Stylesheet string
	}{
		Title:      title,
		Content:    template.HTML(markdown.ToHTML(doc)),
		Stylesheet: web.StaticFS.HashName("static/css/main.css"),
	}

	if err := tmpl.Execute(w, data); err != nil {
		e.respondError(w, fmt.Errorf("executing template: %w", err))
		return
	}
}

func parseTitle(b []byte) (title string, err error) {
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		if strings.HasPrefix(s.Text(), "#") {
			title = strings.TrimPrefix(s.Text(), "# ")
			break
		}
	}
	if err := s.Err(); err != nil && title == "" {
		return "", err
	}
	return title, nil
}

func (e *engine) respondError(w http.ResponseWriter, err error) {
	web.RespondError(e.logf, w, err)
}

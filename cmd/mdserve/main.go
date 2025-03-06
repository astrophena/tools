// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/logger"
	"go.astrophena.name/tools/internal/util/restrict"
	"go.astrophena.name/tools/internal/web"

	"github.com/landlock-lsm/go-landlock/landlock"
	"rsc.io/markdown"
)

//go:embed template.html
var tmpl string

func main() { cli.Main(new(engine)) }

type engine struct {
	init sync.Once
	md   *markdown.Parser

	// configuration
	addr string
	fs   fs.FS
	logf logger.Logf

	// used in tests
	noServerStart bool
}

func (e *engine) Flags(fs *flag.FlagSet) {
	fs.StringVar(&e.addr, "addr", "localhost:3000", "Listen on `host:port`.")
}

func (e *engine) Run(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	var dir string
	if len(env.Args) == 1 {
		dir = env.Args[0]
	}
	if realdir, err := filepath.Abs(dir); err == nil {
		dir = realdir
	}

	var rules []landlock.Rule

	if e.fs == nil && dir != "" {
		e.fs = os.DirFS(dir)
		rules = append(rules, landlock.RODirs(dir))
	}

	_, port, err := net.SplitHostPort(e.addr)
	if err != nil {
		return err
	}
	uport, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return err
	}

	rules = append(rules,
		landlock.BindTCP(uint16(uport)),
		// for DNS
		landlock.ConnectTCP(53),
		landlock.ROFiles("/etc/resolv.conf"),
	)
	if !testing.Testing() {
		restrict.Do(ctx, rules...)
	}

	e.logf = env.Logf

	e.init.Do(e.doInit)

	mux := http.NewServeMux()
	mux.Handle("/", e)

	e.logf("Serving from %s.", e.fs)

	if e.noServerStart {
		return nil
	}

	s := &web.Server{
		Addr: e.addr,
		Mux:  mux,
	}
	return s.ListenAndServe(ctx)
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
		Strikethrough:      true,
		TaskList:           true,
		AutoLinkText:       true,
		AutoLinkAssumeHTTP: true,
		Table:              true,
		SmartDot:           true,
		SmartDash:          true,
		SmartQuote:         true,
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
			web.RespondError(w, r, web.ErrNotFound)
			return
		}
		web.RespondError(w, r, fmt.Errorf("reading file info: %w", err))
		return
	}
	if fi.IsDir() {
		web.RespondError(w, r, web.ErrNotFound)
		return
	}

	b, err := fs.ReadFile(e.fs, p)
	if err != nil {
		web.RespondError(w, r, fmt.Errorf("reading file: %w", err))
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

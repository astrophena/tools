// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
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
	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/internal/restrict"

	"github.com/landlock-lsm/go-landlock/landlock"
	"rsc.io/markdown"
)

var (
	//go:embed template.html
	tmplStr string
	tmpl    = template.Must(template.New("").Parse(tmplStr))
	//go:embed static
	staticFS embed.FS
)

func main() { cli.Main(new(engine)) }

type engine struct {
	init sync.Once
	md   *markdown.Parser
	srv  *web.Server

	// configuration
	addr string
	fs   fs.FS

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
		root, err := os.OpenRoot(dir)
		if err != nil {
			return err
		}
		e.fs = root.FS()
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

	e.init.Do(e.doInit)

	if dir != "" {
		logger.Info(ctx, "serving from", slog.String("dir", dir))
	}

	if e.noServerStart {
		return nil
	}

	return e.srv.ListenAndServe(ctx)
}

func (e *engine) doInit() {
	// Serve by default from current directory.
	if e.fs == nil {
		e.fs = os.DirFS(".")
	}
	e.md = &markdown.Parser{
		HeadingID:          true,
		Strikethrough:      true,
		TaskList:           true,
		AutoLinkText:       true,
		AutoLinkAssumeHTTP: true,
		Table:              true,
		SmartDot:           true,
		SmartDash:          true,
		SmartQuote:         true,
	}

	mux := http.NewServeMux()
	mux.Handle("/", e)

	e.srv = &web.Server{
		Addr:     e.addr,
		Mux:      mux,
		StaticFS: staticFS,
	}
}

func (e *engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.init.Do(e.doInit)

	p := r.URL.Path
	if p == "/" {
		p = e.findIndexFile()
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

	var buf bytes.Buffer
	data := struct {
		Title   string
		Content template.HTML
		AppCSS  string
		AppJS   string
	}{
		Title:   title,
		Content: template.HTML(markdown.ToHTML(doc)),
		AppCSS:  e.srv.StaticHashName("static/css/app.css"),
		AppJS:   e.srv.StaticHashName("static/js/app.js"),
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		web.RespondError(w, r, err)
		return
	}
	buf.WriteTo(w)
}

func (e *engine) findIndexFile() string {
	candidates := []string{
		"index.md",
		"README.md",
	}

	for _, candidate := range candidates {
		if _, err := fs.Stat(e.fs, candidate); err == nil {
			return candidate
		}
	}

	// Fall back to the old behavior.
	return candidates[0]
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

// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"embed"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"strings"
	"sync"

	"go.astrophena.name/base/syncx"
	"go.astrophena.name/base/web"

	"github.com/arl/statsviz"
	"rsc.io/markdown"
)

var (
	//go:embed static/templates/*.tmpl
	templatesFS embed.FS
	//go:embed static/css/* static/js/* static/icons/*
	staticFS embed.FS

	templates = sync.OnceValue(func() *template.Template {
		return template.Must(template.New("").ParseFS(templatesFS, "static/templates/*.tmpl"))
	})
)

var statsvizCSP = web.CSP{
	DefaultSrc:     []string{web.CSPSelf},
	ScriptSrc:      []string{web.CSPSelf},
	StyleSrc:       []string{web.CSPSelf, web.CSPUnsafeInline},
	ConnectSrc:     []string{web.CSPSelf},
	ImgSrc:         []string{web.CSPSelf, "data:"},
	FontSrc:        []string{web.CSPSelf},
	ObjectSrc:      []string{web.CSPNone},
	FrameAncestors: []string{web.CSPNone},
}

var botDocs syncx.Lazy[template.HTML]

func (e *engine) initRoutes() {
	e.mux = http.NewServeMux()
	e.adminMux = http.NewServeMux()

	// Public mux.
	e.mux.HandleFunc("/", e.handlePublicRoot)
	e.mux.HandleFunc("POST /telegram", e.bot.HandleTelegramWebhook)

	// Health check.
	web.Health(e.mux)

	// Starlark environment documentation.
	e.mux.HandleFunc("GET /env", func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer

		docs := botDocs.Get(func() template.HTML {
			parser := &markdown.Parser{
				Strikethrough:      true,
				AutoLinkText:       true,
				AutoLinkAssumeHTTP: true,
				Table:              true,
				SmartDot:           true,
				SmartDash:          true,
				SmartQuote:         true,
			}
			doc := parser.Parse(e.bot.Documentation())
			return template.HTML(markdown.ToHTML(doc))
		})

		data := struct {
			MainCSS       string
			Documentation template.HTML
		}{
			MainCSS:       e.srv.StaticHashName("static/css/main.css"),
			Documentation: docs,
		}
		if err := templates().ExecuteTemplate(&buf, "env.tmpl", data); err != nil {
			web.RespondError(w, r, err)
			return
		}
		buf.WriteTo(w)
	})

	// Admin mux.
	e.adminMux.HandleFunc("/", e.handleAdminRoot)

	// Debug routes.
	dbg := web.Debugger(e.adminMux)
	dbg.MenuFunc(e.debugMenu)
	dbg.KVFunc("Loaded Starlark modules", func() any {
		return fmt.Sprintf("%+v", e.bot.Visited())
	})
	// Runtime metrics.
	statsviz.Register(e.adminMux)
	e.cspMux.Handle("/debug/statsviz/", statsvizCSP)
	dbg.Link("/debug/statsviz", "Metrics")
	// Log streaming.
	dbg.HandleFunc("logs", "Logs", func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		data := struct {
			MainCSS string
			LogsCSS string
			LogsJS  string
		}{
			MainCSS: e.srv.StaticHashName("static/css/main.css"),
			LogsCSS: e.srv.StaticHashName("static/css/logs.css"),
			LogsJS:  e.srv.StaticHashName("static/js/logs.js"),
		}
		if err := templates().ExecuteTemplate(&buf, "logs.tmpl", data); err != nil {
			web.RespondError(w, r, err)
			return
		}
		buf.WriteTo(w)
	})
	e.adminMux.Handle("/debug/log", e.logStream)
	e.adminMux.Handle("GET /debug/loghistory", web.HandleJSON(func(r *http.Request, req any) ([]string, error) {
		return e.logStream.Lines(), nil
	}))

	dbg.HandleFunc("reload", "Reload", func(w http.ResponseWriter, r *http.Request) {
		if err := e.loadFromGist(r.Context()); err != nil {
			web.RespondError(w, r, err)
			return
		}
		http.Redirect(w, r, "/debug/", http.StatusFound)
	})
}

func (e *engine) handlePublicRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		web.RespondError(w, r, web.ErrNotFound)
		return
	}
	const documentationURL = "https://go.astrophena.name/tools/cmd/starlet"
	http.Redirect(w, r, documentationURL, http.StatusFound)
}

func (e *engine) handleAdminRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		web.RespondError(w, r, web.ErrNotFound)
		return
	}
	http.Redirect(w, r, "/debug/", http.StatusFound)
}

func (e *engine) debugMenu(r *http.Request) []web.MenuItem {
	item := func(name, icon, target string) headerItem {
		return headerItem{
			name:       name,
			icon:       icon,
			target:     target,
			spritePath: e.srv.StaticHashName("static/icons/sprite.svg"),
		}
	}

	return []web.MenuItem{
		item("Documentation", "docs", "https://go.astrophena.name/tools/cmd/starlet"),
	}
}

type headerItem struct {
	name       string
	icon       string
	spritePath string
	target     string
}

func (hi headerItem) ToHTML() template.HTML {
	var sb strings.Builder
	sb.WriteString("<a href=")
	sb.WriteString(fmt.Sprintf("%q", hi.target))
	sb.WriteString(">")
	sb.WriteString(fmt.Sprintf(`
<svg class="icon" aria-hidden="true">
  <use xlink:href="/%s#icon-%s"/>
</svg>`, hi.spritePath, hi.icon))
	sb.WriteString(html.EscapeString(hi.name))
	sb.WriteString("</a>")
	return template.HTML(sb.String())
}

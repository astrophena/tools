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
	"go.astrophena.name/tools/internal/store"

	"github.com/arl/statsviz"
	"rsc.io/markdown"
)

var (
	//go:embed static/templates/*.tmpl
	templatesFS embed.FS
	//go:embed static/icons/*
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

	e.mux.HandleFunc("/", e.handlePublicRoot)
	e.mux.HandleFunc("POST /telegram", e.bot.HandleTelegramWebhook)

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
			MainCSS:       web.StaticHashName(r.Context(), "static/css/main.css"),
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
	if s, ok := e.store.(*store.JSONFile); ok {
		dbg.KVFunc("KV cache store", func() any {
			stats := s.Stats()
			return fmt.Sprintf(
				"path=%s\nmetrics=%s\nttl=%s\nsession_gets=%d\nsession_sets=%d\nsession_rewrites=%d\nsession_rewrite_bytes=%s (%d)\ntotal_gets=%d\ntotal_sets=%d\ntotal_rewrites=%d\ntotal_rewrite_bytes=%s (%d)\ncurrent_size=%s (%d)\nexpired=%d\ncleanup_deletes=%d",
				stats.Path,
				stats.MetricsPath,
				stats.TTL,
				stats.Gets,
				stats.Sets,
				stats.Rewrites,
				humanBytes(stats.RewriteBytes),
				stats.RewriteBytes,
				stats.TotalGets,
				stats.TotalSets,
				stats.TotalRewrites,
				humanBytes(stats.TotalRewriteBytes),
				stats.TotalRewriteBytes,
				humanBytes(uint64(stats.FileSizeBytes)),
				stats.FileSizeBytes,
				stats.TotalExpired,
				stats.TotalCleanupDeletes,
			)
		})
	}
	// Runtime metrics.
	statsviz.Register(e.adminMux)
	e.cspMux.Handle("/debug/statsviz/", statsvizCSP)
	dbg.Link("/debug/statsviz", "Metrics")

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
			spritePath: web.StaticHashName(r.Context(), "static/icons/sprite.svg"),
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

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for q := n / unit; q >= unit; q /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

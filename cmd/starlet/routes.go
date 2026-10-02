// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:generate go tool templ fmt .
//go:generate go tool templ generate -include-version=false

package main

import (
	"bytes"
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"go.astrophena.name/base/humanfmt"
	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/internal/store"

	"rsc.io/markdown"
)

//go:embed static/icons/*
var staticFS embed.FS

const documentationURL = "https://go.astrophena.name/tools/cmd/starlet"

func (e *engine) initRoutes() {
	e.mux = http.NewServeMux()
	e.adminMux = http.NewServeMux()

	e.mux.HandleFunc("/", e.handlePublicRoot)
	e.mux.HandleFunc("POST /telegram", e.bot.HandleTelegramWebhook)

	// Starlark environment documentation, cached for this engine.
	docs := sync.OnceValue(func() string {
		parser := &markdown.Parser{
			Strikethrough:      true,
			AutoLinkText:       true,
			AutoLinkAssumeHTTP: true,
			Table:              true,
			SmartDot:           true,
			SmartDash:          true,
			SmartQuote:         true,
		}
		return markdown.ToHTML(parser.Parse(e.bot.Documentation()))
	})
	e.mux.HandleFunc("GET /env", func(w http.ResponseWriter, r *http.Request) {
		css := web.StaticHashName(r.Context(), "static/css/main.css")
		var buf bytes.Buffer
		if err := environmentPage(css, docs()).Render(r.Context(), &buf); err != nil {
			web.RespondError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
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
				humanfmt.Bytes(stats.RewriteBytes),
				stats.RewriteBytes,
				stats.TotalGets,
				stats.TotalSets,
				stats.TotalRewrites,
				humanfmt.Bytes(stats.TotalRewriteBytes),
				stats.TotalRewriteBytes,
				humanfmt.Bytes(uint64(stats.FileSizeBytes)),
				stats.FileSizeBytes,
				stats.TotalExpired,
				stats.TotalCleanupDeletes,
			)
		})
	}
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
	sprite := web.StaticHashName(r.Context(), "static/icons/sprite.svg")
	var buf bytes.Buffer
	if err := documentationLink(sprite).Render(r.Context(), &buf); err != nil {
		logger.Error(r.Context(), "rendering debug menu", slog.Any("err", err))
		return []web.MenuItem{web.LinkItem{Name: "Documentation", Target: documentationURL}}
	}
	// The debugger accepts trusted HTML rather than templ components.
	return []web.MenuItem{web.HTMLItem(buf.String())}
}

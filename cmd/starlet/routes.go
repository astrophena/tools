// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"crypto/subtle"
	"embed"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"os"
	"strings"

	"go.astrophena.name/base/tgauth"
	"go.astrophena.name/base/web"
)

var (
	//go:embed static/templates/logs.tmpl
	logsTmpl string
	//go:embed static/js/* static/icons/*
	staticFS embed.FS
)

func (e *engine) initRoutes() {
	e.mux = http.NewServeMux()

	e.mux.HandleFunc("/", e.handleRoot)
	e.mux.HandleFunc("POST /telegram", e.bot.HandleTelegramWebhook)
	e.mux.HandleFunc("POST /reload", e.handleReload)

	// Authentication.
	e.mux.Handle("GET /login", e.tgAuth.LoginHandler("/debug/"))
	e.mux.Handle("GET /logout", e.tgAuth.LogoutHandler("/"))

	// Health check.
	web.Health(e.mux)

	// Debug routes.
	dbg := web.Debugger(e.mux)
	dbg.MenuFunc(e.debugMenu)
	dbg.KVFunc("Bot information", func() any { return fmt.Sprintf("%+v", e.me) })
	dbg.KVFunc("Loaded Starlark modules", func() any {
		return fmt.Sprintf("%+v", e.bot.Visited())
	})
	// Log streaming.
	dbg.HandleFunc("logs", "Logs", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(
			w,
			logsTmpl,
			html.EscapeString(strings.Join(e.logStream.Lines(), "")),
			e.srv.StaticHashName("static/css/main.css"),
			e.srv.StaticHashName("static/js/logs.js"),
		)
	})
	e.mux.Handle("/debug/log", e.logStream)
	dbg.HandleFunc("reload", "Reload from gist", func(w http.ResponseWriter, r *http.Request) {
		if err := e.bot.LoadFromGist(r.Context()); err != nil {
			web.RespondError(w, r, err)
			return
		}
		http.Redirect(w, r, "/debug/", http.StatusFound)
	})

	// Redirect from *.onrender.com to bot host.
	if e.onRender && e.host != "" {
		if onRenderHost := os.Getenv("RENDER_EXTERNAL_HOSTNAME"); onRenderHost != "" {
			e.mux.HandleFunc(onRenderHost+"/", func(w http.ResponseWriter, r *http.Request) {
				targetURL := "https://" + e.host + r.URL.Path
				http.Redirect(w, r, targetURL, http.StatusMovedPermanently)
			})
		}
	}
}

func (e *engine) handleRoot(w http.ResponseWriter, r *http.Request) {
	const documentationURL = "https://go.astrophena.name/tools/cmd/starlet"
	if r.URL.Path != "/" {
		web.RespondError(w, r, web.ErrNotFound)
		return
	}
	if e.tgAuth.LoggedIn(r) {
		http.Redirect(w, r, "/debug/", http.StatusFound)
		return
	}
	http.Redirect(w, r, documentationURL, http.StatusFound)
}

func (e *engine) handleReload(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(tok), []byte(e.reloadToken)) != 1 {
		web.RespondJSONError(w, r, web.ErrUnauthorized)
		return
	}
	if err := e.bot.LoadFromGist(r.Context()); err != nil {
		web.RespondJSONError(w, r, err)
		return
	}
	web.RespondJSON(w, map[string]string{"status": "success"})
}

func (e *engine) debugMenu(r *http.Request) []web.MenuItem {
	ident := tgauth.Identify(r)
	if ident == nil {
		return nil
	}

	item := func(name, icon, target string) headerItem {
		return headerItem{
			name:       name,
			icon:       icon,
			target:     target,
			spritePath: e.srv.StaticHashName("static/icons/sprite.svg"),
		}
	}

	nameLink := item(ident.FirstName, "user", "https://t.me/"+ident.Username)
	if ident.LastName != "" {
		nameLink.name += " " + ident.LastName
	}

	return []web.MenuItem{
		nameLink,
		item("Documentation", "docs", "https://go.astrophena.name/tools/cmd/starlet"),
		item("Log out", "logout", "/logout"),
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

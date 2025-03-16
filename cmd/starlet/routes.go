// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	_ "embed"
	"fmt"
	"html"
	"net/http"
	"os"
	"strings"

	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/cmd/starlet/internal/geminiproxy"
	"go.astrophena.name/tools/cmd/starlet/internal/tgauth"
)

var (
	//go:embed assets/logs.html
	logsTmpl string
	//go:embed assets/logs.js
	logsJS []byte
)

func (e *engine) initRoutes() {
	e.mux = http.NewServeMux()

	e.mux.HandleFunc("/", e.handleRoot)
	e.mux.HandleFunc("POST /telegram", e.handleTelegramWebhook)
	e.mux.HandleFunc("POST /reload", e.handleReload)
	if e.geminic != nil && e.geminiProxyToken != "" {
		e.mux.Handle("/gemini/", http.StripPrefix("/gemini", geminiproxy.Handler(e.geminiProxyToken, e.geminic)))
	}

	// Authentication.
	e.mux.Handle("GET /login", e.tgAuth.LoginHandler("/debug/"))
	e.mux.Handle("GET /logout", e.tgAuth.LogoutHandler("/"))

	// Debug routes.
	web.Health(e.mux)
	dbg := web.Debugger(e.mux)
	dbg.MenuFunc(e.debugMenu)
	dbg.KVFunc("Bot information", func() any { return fmt.Sprintf("%+v", e.me) })
	dbg.KVFunc("Loaded Starlark modules", func() any {
		return fmt.Sprintf("%+v", e.bot.Load().intr.Visited())
	})
	// Log streaming.
	dbg.HandleFunc("logs", "Logs", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, logsTmpl, html.EscapeString(strings.Join(e.logStream.Lines(), "")), web.StaticFS.HashName("static/css/main.css"))
	})
	e.mux.HandleFunc("/debug/logs.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(logsJS)
	})
	e.mux.Handle("/debug/log", e.logStream)
	dbg.HandleFunc("reload", "Reload from gist", func(w http.ResponseWriter, r *http.Request) {
		if err := e.loadFromGist(r.Context()); err != nil {
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
	if tok != e.reloadToken {
		web.RespondJSONError(w, r, web.ErrUnauthorized)
		return
	}
	if err := e.loadFromGist(r.Context()); err != nil {
		web.RespondJSONError(w, r, err)
		return
	}
	jsonOK(w)
}

func (e *engine) debugMenu(r *http.Request) []web.MenuItem {
	ident := tgauth.Identify(r)
	if ident == nil {
		return nil
	}
	fullName := ident.FirstName
	if ident.LastName != "" {
		fullName += " " + ident.LastName
	}
	return []web.MenuItem{
		web.HTMLItem(fmt.Sprintf("Logged in as %s (ID: %d)", fullName, ident.ID)),
		web.LinkItem{
			Name:   "Documentation",
			Target: "https://go.astrophena.name/tools/cmd/starlet",
		},
		web.LinkItem{
			Name:   "Log out",
			Target: "/logout",
		},
	}
}

func jsonOK(w http.ResponseWriter) {
	var res struct {
		Status string `json:"status"`
	}
	res.Status = "success"
	web.RespondJSON(w, res)
}

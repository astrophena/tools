// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"fmt"
	"html"
	"net/http"
	"os"
	"strings"

	"go.astrophena.name/tools/cmd/starlet/internal/geminiproxy"
	"go.astrophena.name/tools/cmd/starlet/internal/tgauth"
	"go.astrophena.name/tools/internal/web"
)

func (e *engine) initRoutes() {
	e.mux = http.NewServeMux()

	e.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			web.RespondError(w, r, web.ErrNotFound)
			return
		}
		if e.tgAuth.LoggedIn(r) {
			http.Redirect(w, r, "/debug/", http.StatusFound)
			return
		}
		http.Redirect(w, r, "https://go.astrophena.name/tools/cmd/starlet", http.StatusFound)
	})

	e.mux.HandleFunc("POST /telegram", e.handleTelegramWebhook)
	e.mux.HandleFunc("POST /reload", e.handleReload)
	if e.geminic != nil && e.geminiProxyToken != "" {
		e.mux.Handle("/gemini/", http.StripPrefix("/gemini", geminiproxy.Handler(e.geminiProxyToken, e.geminic)))
	}

	// Redirect from *.onrender.com to bot host.
	if e.onRender && e.host != "" {
		if onRenderHost := os.Getenv("RENDER_EXTERNAL_HOSTNAME"); onRenderHost != "" {
			e.mux.HandleFunc(onRenderHost+"/", func(w http.ResponseWriter, r *http.Request) {
				targetURL := "https://" + e.host + r.URL.Path
				http.Redirect(w, r, targetURL, http.StatusMovedPermanently)
			})
		}
	}

	// Authentication.
	e.mux.Handle("GET /login", e.tgAuth.LoginHandler("/debug/"))
	e.mux.Handle("GET /logout", e.tgAuth.LogoutHandler("/"))

	// Debug routes.
	web.Health(e.mux)
	dbg := web.Debugger(e.mux)

	dbg.MenuFunc(func(r *http.Request) []web.MenuItem {
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
	})

	dbg.KVFunc("Bot information", func() any {
		me, err := e.getMe()
		if err != nil {
			return err
		}
		return fmt.Sprintf("%+v", me)
	})

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

func jsonOK(w http.ResponseWriter) {
	var res struct {
		Status string `json:"status"`
	}
	res.Status = "success"
	web.RespondJSON(w, res)
}

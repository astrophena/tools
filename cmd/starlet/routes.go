// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.astrophena.name/base/syncx"
	"go.astrophena.name/base/web"
	"go.astrophena.name/base/web/tgauth"
	"go.astrophena.name/tools/cmd/starlet/internal/geminiproxy"
	"go.astrophena.name/tools/internal/api/gemini"

	"github.com/arl/statsviz"
	"github.com/dchest/uniuri"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/time/rate"
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

	e.mux.HandleFunc("/", e.handleRoot)
	e.mux.HandleFunc("POST /telegram", e.bot.HandleTelegramWebhook)
	e.mux.HandleFunc("POST /reload", e.handleReload)

	// Authentication.
	e.mux.Handle("GET /login", e.tgAuth.LoginHandler("/debug/"))
	e.mux.Handle("GET /logout", e.tgAuth.LogoutHandler("/"))

	// Health check.
	web.Health(e.mux)

	// Gemini proxy.
	if e.geminiProxySecretKey != "" && e.geminiKey != "" {
		e.mux.Handle("/gemini/", http.StripPrefix("/gemini", geminiproxy.Handler(e.geminiProxySecretKey, &gemini.Client{
			APIKey:     e.geminiKey,
			HTTPClient: e.httpc,
		})))
	}

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

	// Debug routes.
	dbg := web.Debugger(e.mux)
	dbg.MenuFunc(e.debugMenu)
	dbg.KVFunc("Loaded Starlark modules", func() any {
		return fmt.Sprintf("%+v", e.bot.Visited())
	})
	// Runtime metrics.
	statsviz.Register(e.mux)
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
	e.mux.Handle("/debug/log", e.logStream)
	e.mux.Handle("GET /debug/loghistory", web.HandleJSON(func(r *http.Request, req any) ([]string, error) {
		return e.logStream.Lines(), nil
	}))

	if e.dev {
		// Bot debugger.
		dbg.HandleFunc("bot", "Bot debugger", func(w http.ResponseWriter, r *http.Request) {
			var buf bytes.Buffer
			data := struct {
				MainCSS string
				BotCSS  string
				BotJS   string
			}{
				MainCSS: e.srv.StaticHashName("static/css/main.css"),
				BotCSS:  e.srv.StaticHashName("static/css/bot.css"),
				BotJS:   e.srv.StaticHashName("static/js/bot.js"),
			}
			if err := templates().ExecuteTemplate(&buf, "bot.tmpl", data); err != nil {
				web.RespondError(w, r, err)
				return
			}
			buf.WriteTo(w)
		})
		e.mux.Handle("/debug/events", e.tgInterceptor)
	}

	dbg.HandleFunc("reload", "Reload", func(w http.ResponseWriter, r *http.Request) {
		var err error
		if e.dev {
			err = e.loadFromDir(r.Context())
		} else {
			err = e.loadFromGist(r.Context())
		}
		if err != nil {
			web.RespondError(w, r, err)
			return
		}
		http.Redirect(w, r, "/debug/", http.StatusFound)
	})

	if e.geminiProxySecretKey != "" {
		dbg.HandleFunc("gemini-token", "Generate Gemini proxy token", e.handleGeminiProxyToken)
		dbg.HandleFunc("gemini-proxy-stats", "Gemini proxy stats", e.handleGeminiProxyStats)
		e.mux.HandleFunc("/debug/gemini-proxy-stats-stream", e.handleGeminiProxyStatsStream)
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
}

func (e *engine) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		web.RespondError(w, r, web.ErrNotFound)
		return
	}
	if e.dev {
		http.Redirect(w, r, "/debug/bot", http.StatusFound)
		return
	}
	const documentationURL = "https://go.astrophena.name/tools/cmd/starlet"
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

	var err error
	if e.dev {
		err = e.loadFromDir(r.Context())
	} else {
		err = e.loadFromGist(r.Context())
	}
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	web.RespondJSON(w, map[string]string{"status": "success"})
}

func (e *engine) handleGeminiProxyToken(w http.ResponseWriter, r *http.Request) {
	if e.geminiProxySecretKey == "" {
		// This should not happen because we check for it before setting up the
		// handler, but just in case.
		web.RespondError(w, r, errors.New("GEMINI_PROXY_SECRET_KEY is not configured"))
		return
	}

	var (
		token    string
		duration = "24h"
	)

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			web.RespondError(w, r, err)
			return
		}

		d, err := time.ParseDuration(r.FormValue("duration"))
		if err != nil {
			web.RespondError(w, r, err)
			return
		}
		duration = r.FormValue("duration")

		rateLimit, err := strconv.ParseFloat(r.FormValue("rate_limit"), 64)
		if err != nil || rateLimit <= 0 {
			rateLimit = 1
		}

		claims := geminiproxy.Claims{
			Description: r.FormValue("description"),
			RateLimit:   rate.Limit(rateLimit),
			RegisteredClaims: jwt.RegisteredClaims{
				ID:        uniuri.New(),
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(d)),
			},
		}
		t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		token, err = t.SignedString([]byte(e.geminiProxySecretKey))
		if err != nil {
			web.RespondError(w, r, err)
			return
		}
	}

	var buf bytes.Buffer
	data := struct {
		MainCSS         string
		GeminiTokenCSS  string
		GeminiTokenJS   string
		DefaultDuration string
		Duration        string
		Token           string
	}{
		MainCSS:         e.srv.StaticHashName("static/css/main.css"),
		GeminiTokenCSS:  e.srv.StaticHashName("static/css/gemini-token.css"),
		GeminiTokenJS:   e.srv.StaticHashName("static/js/gemini-token.js"),
		DefaultDuration: duration,
		Duration:        duration,
		Token:           token,
	}
	if err := templates().ExecuteTemplate(&buf, "gemini-token.tmpl", data); err != nil {
		web.RespondError(w, r, err)
		return
	}
	buf.WriteTo(w)
}

func (e *engine) handleGeminiProxyStats(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	data := struct {
		MainCSS  string
		StatsCSS string
		StatsJS  string
	}{
		MainCSS:  e.srv.StaticHashName("static/css/main.css"),
		StatsCSS: e.srv.StaticHashName("static/css/gemini-proxy-stats.css"),
		StatsJS:  e.srv.StaticHashName("static/js/gemini-proxy-stats.js"),
	}
	if err := templates().ExecuteTemplate(&buf, "gemini-proxy-stats.tmpl", data); err != nil {
		web.RespondError(w, r, err)
		return
	}
	buf.WriteTo(w)
}

func (e *engine) handleGeminiProxyStatsStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	type statLine struct {
		ID          string `json:"id"`
		Description string `json:"description"`
		Requests    uint64 `json:"requests"`
		Limit       string `json:"limit"`
		LastUsed    string `json:"last_used"`
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			var (
				lines   []statLine
				flusher http.Flusher
				ok      bool
			)
			if flusher, ok = w.(http.Flusher); !ok {
				web.RespondError(w, r, errors.New("streaming unsupported"))
				return
			}

			geminiproxy.RangeStats(func(id string, stat *geminiproxy.TokenStat) bool {
				var lastUsed string
				if lu := stat.LastUsed(); lu != nil {
					lastUsed = lu.Format(time.RFC3339)
				} else {
					lastUsed = "never"
				}

				lines = append(lines, statLine{
					ID:          id,
					Description: stat.Description(),
					Requests:    stat.Requests(),
					Limit:       fmt.Sprintf("%v req/s", stat.Limit()),
					LastUsed:    lastUsed,
				})
				return true
			})

			var buf bytes.Buffer
			if err := json.NewEncoder(&buf).Encode(lines); err != nil {
				e.logger.Error("failed to encode gemini proxy stats", "err", err)
				return
			}

			fmt.Fprintf(w, "data: %s\n\n", buf.String())
			flusher.Flush()
		}
	}
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

type tgInterceptor struct {
	realTransport http.RoundTripper
	mu            sync.RWMutex
	clients       map[chan []byte]struct{}
	logger        *slog.Logger
}

func newTgInterceptor(logger *slog.Logger, realTransport http.RoundTripper) *tgInterceptor {
	return &tgInterceptor{
		realTransport: realTransport,
		clients:       make(map[chan []byte]struct{}),
		logger:        logger,
	}
}

func (i *tgInterceptor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan []byte)
	i.mu.Lock()
	i.clients[ch] = struct{}{}
	i.mu.Unlock()

	defer func() {
		i.mu.Lock()
		delete(i.clients, ch)
		i.mu.Unlock()
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		web.RespondError(w, r, errors.New("streaming unsupported"))
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

func (i *tgInterceptor) RoundTrip(r *http.Request) (*http.Response, error) {
	// Intercept only Telegram Bot API requests.
	if r.URL.Host != "api.telegram.org" {
		return i.realTransport.RoundTrip(r)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body.Close()

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		i.logger.Error("unmarshaling intercepted request body failed", "err", err)
	}

	event := map[string]any{
		"url":    r.URL.String(),
		"method": path.Base(r.URL.Path),
		"body":   data,
	}

	b, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	i.mu.RLock()
	defer i.mu.RUnlock()
	for ch := range i.clients {
		ch <- b
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)),
		Header:     make(http.Header),
	}, nil
}

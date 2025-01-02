// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// vim: foldmethod=marker

package main

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/request"
	"go.astrophena.name/tools/cmd/starlet/internal/convcache"
	"go.astrophena.name/tools/cmd/starlet/internal/starlarkgemini"
	"go.astrophena.name/tools/cmd/starlet/internal/tgauth"
	"go.astrophena.name/tools/cmd/starlet/internal/tgmarkup"
	"go.astrophena.name/tools/cmd/starlet/internal/tgstarlark"
	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/api/google/gemini"
	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/util/logstream"
	"go.astrophena.name/tools/internal/util/starlarkconv"
	"go.astrophena.name/tools/internal/util/syncx"
	"go.astrophena.name/tools/internal/version"
	"go.astrophena.name/tools/internal/web"

	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

const tgAPI = "https://api.telegram.org"

var (
	//go:embed resources/error.tmpl
	defaultErrorTemplate string
	//go:embed resources/logs.html
	logsTmpl string
	//go:embed resources/logs.js
	logsJS []byte
)

var selfPingInterval = 10 * time.Minute

func main() { cli.Main(new(engine)) }

func (e *engine) Flags(fs *flag.FlagSet) {
	fs.Int64Var(&e.tgOwner, "tg-owner", 0, "Telegram user `ID` of the bot owner.")
	fs.StringVar(&e.addr, "addr", "localhost:3000", "Listen on `host:port`.")
	fs.StringVar(&e.geminiKey, "gemini-key", "", "Gemini API `key`.")
	fs.StringVar(&e.ghToken, "gh-token", "", "GitHub API `token`.")
	fs.StringVar(&e.gistID, "gist-id", "", "GitHub Gist `ID` to load bot code from.")
	fs.StringVar(&e.host, "host", "", "Bot `domain` used for setting up webhook.")
	fs.StringVar(&e.reloadToken, "reload-token", "", "Secret `token` used for calling /reload endpoint.")
	fs.StringVar(&e.tgSecret, "tg-secret", "", "Secret `token` used to validate Telegram Bot API updates.")
	fs.StringVar(&e.tgToken, "tg-token", "", "Telegram Bot API `token`.")
}

func (e *engine) Run(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	// Load configuration from environment variables or flags.
	e.geminiKey = cmp.Or(env.Getenv("GEMINI_KEY"), e.geminiKey)
	e.ghToken = cmp.Or(env.Getenv("GH_TOKEN"), e.ghToken)
	e.gistID = cmp.Or(env.Getenv("GIST_ID"), e.gistID)
	e.host = cmp.Or(env.Getenv("HOST"), e.host)
	e.onRender = env.Getenv("RENDER") == "true"
	e.reloadToken = cmp.Or(env.Getenv("RELOAD_TOKEN"), e.reloadToken)
	e.tgOwner = cmp.Or(parseInt(env.Getenv("TG_OWNER")), e.tgOwner)
	e.tgSecret = cmp.Or(env.Getenv("TG_SECRET"), e.tgSecret)
	e.tgToken = cmp.Or(env.Getenv("TG_TOKEN"), e.tgToken)

	// Initialize internal state.
	e.stderr = env.Stderr
	if err := e.init.Get(e.doInit); err != nil {
		return err
	}

	// Used in tests.
	if e.noServerStart {
		return nil
	}

	// If running on Render, try to look up port to listen on, activate webhook
	// and start goroutine that prevents Starlet from sleeping.
	if e.onRender {
		// https://docs.render.com/environment-variables#all-runtimes-1
		if port := env.Getenv("PORT"); port != "" {
			e.addr = ":" + port
		}
		if err := e.setWebhook(ctx); err != nil {
			return err
		}
		go e.selfPing(ctx, env.Getenv, selfPingInterval)
	}

	return web.ListenAndServe(ctx, &web.ListenAndServeConfig{
		Addr:       e.addr,
		Debuggable: true, // debug endpoints protected by Telegram auth
		Mux:        e.mux,
		Ready:      e.ready,
		Middleware: []func(http.Handler) http.Handler{
			e.tgAuth.Middleware(false),
			e.debugAuth,
		},
	})
}

func parseInt(s string) int64 {
	i, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		return i
	}
	return 0
}

type engine struct {
	init     syncx.Lazy[error] // main initialization
	loadGist sync.Once         // lazily loads gist when first webhook request arrives

	// initialized by doInit
	convCache *starlarkstruct.Module
	geminic   *gemini.Client
	gistc     *gist.Client
	scrubber  *strings.Replacer
	logStream logstream.Streamer
	logf      logger.Logf
	mux       *http.ServeMux
	tgAuth    *tgauth.Middleware

	// configuration, read-only after initialization
	addr          string
	geminiKey     string
	ghToken       string
	gistID        string
	host          string
	httpc         *http.Client
	onRender      bool
	reloadToken   string
	stderr        io.Writer
	tgBotID       int64
	tgBotUsername string
	tgOwner       int64
	tgSecret      string
	tgToken       string
	// for tests
	noServerStart bool
	ready         func() // see web.ListenAndServeConfig.Ready

	mu sync.RWMutex
	// loaded from gist
	loadGistErr   error
	bot           []byte
	files         map[string]string
	botProg       starlark.StringDict
	errorTemplate string
}

func (e *engine) doInit() error {
	if e.httpc == nil {
		e.httpc = &http.Client{
			// Increase timeout to properly handle Gemini API response times.
			Timeout: 60 * time.Second,
		}
	}
	if e.stderr == nil {
		e.stderr = os.Stderr
	}

	e.convCache = convcache.Module(24 * time.Hour)

	const logLineLimit = 300
	e.logStream = logstream.New(logLineLimit)
	e.logf = log.New(io.MultiWriter(e.stderr, &timestampWriter{e.logStream}), "", 0).Printf

	var scrubPairs []string
	for _, val := range []string{
		e.ghToken,
		e.gistID,
		e.tgSecret,
		e.tgToken,
		e.geminiKey,
	} {
		if val != "" {
			scrubPairs = append(scrubPairs, val, "[EXPUNGED]")
		}
	}
	// Quick sanity check.
	if len(scrubPairs)%2 != 0 {
		panic("scrubPairs are not even; check doInit method on engine")
	}
	if len(scrubPairs) > 0 {
		e.scrubber = strings.NewReplacer(scrubPairs...)
	}

	e.gistc = &gist.Client{
		Token:      e.ghToken,
		HTTPClient: e.httpc,
		Scrubber:   e.scrubber,
	}
	if e.geminiKey != "" {
		e.geminic = &gemini.Client{
			APIKey:     e.geminiKey,
			HTTPClient: e.httpc,
			Scrubber:   e.scrubber,
		}
	}
	e.tgAuth = &tgauth.Middleware{
		CheckFunc: e.authCheck,
		Token:     e.tgToken,
	}

	me, err := e.getMe()
	if err != nil {
		return err
	}
	e.tgBotID = me.Result.ID
	e.tgBotUsername = me.Result.Username

	e.initRoutes()

	return nil
}

func (e *engine) getMe() (getMeResponse, error) {
	return request.Make[getMeResponse](context.Background(), request.Params{
		Method:     http.MethodGet,
		URL:        tgAPI + "/bot" + e.tgToken + "/getMe",
		HTTPClient: e.httpc,
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
		Scrubber: e.scrubber,
	})
}

type getMeResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		ID                      int64  `json:"id"`
		IsBot                   bool   `json:"is_bot"`
		FirstName               string `json:"first_name"`
		Username                string `json:"username"`
		CanJoinGroups           bool   `json:"can_join_groups"`
		CanReadAllGroupMessages bool   `json:"can_read_all_group_messages"`
		SupportsInlineQueries   bool   `json:"supports_inline_queries"`
		CanConnectToBusiness    bool   `json:"can_connect_to_business"`
		HasMainWebApp           bool   `json:"has_main_web_app"`
	} `json:"result"`
}

// timestampWriter is an io.Writer that prefixes each line with the current date and time.
type timestampWriter struct {
	w io.Writer
}

// Write implements the [io.Writer] interface.
func (tw *timestampWriter) Write(p []byte) (n int, err error) {
	lines := bytes.SplitAfter(p, []byte{'\n'})

	for _, line := range lines {
		if len(line) > 0 {
			timestamp := time.Now().Format(time.DateTime + "\t")
			_, err := tw.w.Write([]byte(timestamp))
			if err != nil {
				return n, err
			}
			nn, err := tw.w.Write(line)
			n += nn
			if err != nil {
				return n, err
			}
		}
	}

	return n, nil
}

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

	dbg.HandleFunc("code", "Bot code", func(w http.ResponseWriter, r *http.Request) {
		if err := e.ensureLoaded(r.Context()); err != nil {
			web.RespondError(w, r, err)
			return
		}
		e.mu.RLock()
		defer e.mu.RUnlock()
		w.Write(e.bot)
	})

	dbg.HandleFunc("logs", "Logs", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, logsTmpl, strings.Join(e.logStream.Lines(), ""), web.StaticFS.HashName("static/css/main.css"))
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

func (e *engine) debugAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/debug") {
			next.ServeHTTP(w, r)
			return
		}
		if !e.onRender {
			next.ServeHTTP(w, r)
			return
		}
		if e.tgAuth.LoggedIn(r) {
			next.ServeHTTP(w, r)
			return
		}
		web.RespondError(w, r, web.ErrUnauthorized)
	})
}

func (e *engine) authCheck(ident *tgauth.Identity) bool {
	// Check if ID of authenticated user matches the bot owner ID.
	if ident.ID != e.tgOwner {
		return false
	}
	// Check if auth data was not created more that 24 hours ago.
	return time.Since(ident.AuthDate) < 24*time.Hour
}

func (e *engine) ensureLoaded(ctx context.Context) error {
	e.loadGist.Do(func() { e.loadGistErr = e.loadFromGist(ctx) })
	return e.loadGistErr
}

func (e *engine) loadFromGist(ctx context.Context) error {
	g, err := e.gistc.Get(ctx, e.gistID)
	if err != nil {
		return err
	}
	files := make(map[string]string)
	for name, file := range g.Files {
		files[name] = file.Content
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.loadCode(ctx, files)
}

// e.mu must be held for writing.
func (e *engine) loadCode(ctx context.Context, files map[string]string) error {
	bot, exists := files["bot.star"]
	if !exists {
		return errors.New("bot.star should contain bot code")
	}
	botCode := []byte(bot)

	botProg, err := starlark.ExecFileOptions(
		&syntax.FileOptions{},
		e.newStarlarkThread(context.Background()),
		"bot.star",
		botCode,
		e.predeclared(),
	)
	if err != nil {
		return err
	}

	if hook, ok := botProg["on_load"]; ok {
		_, err = starlark.Call(e.newStarlarkThread(ctx), hook, starlark.Tuple{}, nil)
		if err != nil {
			return err
		}
	}

	if errorTmpl, exists := files["error.tmpl"]; exists {
		e.errorTemplate = errorTmpl
	} else {
		e.errorTemplate = defaultErrorTemplate
	}
	e.bot = botCode
	e.botProg = botProg
	e.files = files
	e.loadGistErr = nil

	return nil
}

func (e *engine) predeclared() starlark.StringDict {
	return starlark.StringDict{
		"config": starlarkstruct.FromStringDict(
			starlarkstruct.Default,
			starlark.StringDict{
				"bot_id":       starlark.MakeInt64(e.tgBotID),
				"bot_username": starlark.String(e.tgBotUsername),
				"owner_id":     starlark.MakeInt64(e.tgOwner),
				"version":      starlark.String(version.Version().String()),
			},
		),
		// Undocumented functions, mostly for debugging.
		"debug": starlarkstruct.FromStringDict(
			starlarkstruct.Default,
			starlark.StringDict{
				"stack":    starlark.NewBuiltin("debug.stack", getStarlarkStack),
				"go_stack": starlark.NewBuiltin("debug.go_stack", getGoStack),
			},
		),
		"convcache": e.convCache,
		"files": &starlarkstruct.Module{
			Name: "files",
			Members: starlark.StringDict{
				"read": starlark.NewBuiltin("files.read", e.readFile),
			},
		},
		"gemini": starlarkgemini.Module(e.geminic),
		"markdown": &starlarkstruct.Module{
			Name: "markdown",
			Members: starlark.StringDict{
				"convert": starlark.NewBuiltin("markdown.convert", convertMarkdown),
			},
		},
		"telegram": tgstarlark.Module(e.tgToken, e.httpc),
		"time":     starlarktime.Module,
	}
}

func (e *engine) newStarlarkThread(ctx context.Context) *starlark.Thread {
	thread := &starlark.Thread{
		Print: func(thread *starlark.Thread, msg string) { e.logf("%s", msg) },
	}
	if ctx != nil {
		thread.SetLocal("context", ctx)
	}
	return thread
}

var errNoHandleFunc = errors.New("handle function not found in bot code")

func (e *engine) handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != e.tgSecret {
		web.RespondJSONError(w, r, web.ErrNotFound)
		return
	}

	b, err := io.ReadAll(r.Body)
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}
	var gu map[string]any
	if err := json.Unmarshal(b, &gu); err != nil {
		web.RespondJSONError(w, r, err)
		return
	}
	u, err := starlarkconv.ToValue(gu)
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	chatID := e.lookupChatID(gu) // for error reports

	if err := e.ensureLoaded(r.Context()); err != nil {
		e.reportError(r.Context(), chatID, w, err)
		return
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	f, ok := e.botProg["handle"]
	if !ok {
		e.reportError(r.Context(), chatID, w, errNoHandleFunc)
		return
	}

	_, err = starlark.Call(e.newStarlarkThread(r.Context()), f, starlark.Tuple{u}, nil)
	if err != nil {
		e.reportError(r.Context(), chatID, w, err)
		return
	}

	jsonOK(w)
}

func (e *engine) lookupChatID(update map[string]any) int64 {
	msg, ok := update["message"].(map[string]any)
	if !ok {
		return e.tgOwner
	}

	chat, ok := msg["chat"].(map[string]any)
	if !ok {
		return e.tgOwner
	}

	id, ok := chat["id"].(int64)
	if ok {
		return id
	}

	fid, ok := chat["id"].(float64)
	if ok {
		return int64(fid)
	}

	return e.tgOwner
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

// Starlark builtins {{{

// debug.stack Starlark function.
func getStarlarkStack(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 || len(kwargs) > 0 {
		return nil, errors.New("unexpected arguments")
	}
	return starlark.String(thread.CallStack().String()), nil
}

// debug.go_stack Starlark function.
func getGoStack(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 || len(kwargs) > 0 {
		return nil, errors.New("unexpected arguments")
	}
	return starlark.String(string(debug.Stack())), nil
}

// markdown.convert Starlark function.
func convertMarkdown(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "s", &s); err != nil {
		return starlark.None, err
	}
	return starlarkconv.ToValue(tgmarkup.FromMarkdown(s))
}

// files.read Starlark function. e.mu must be held for reading.
func (e *engine) readFile(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name", &name); err != nil {
		return starlark.None, err
	}
	file, exists := e.files[name]
	if !exists {
		return starlark.None, fmt.Errorf("file %s not found in Gist", name)
	}
	return starlark.String(file), nil
}

// }}}

// Render environment {{{

var errNoHost = errors.New("host hasn't set; pass it with -host flag or HOST environment variable")

func (e *engine) setWebhook(ctx context.Context) error {
	if e.host == "" {
		return errNoHost
	}
	u := &url.URL{
		Scheme: "https",
		Host:   e.host,
		Path:   "/telegram",
	}
	_, err := request.Make[any](ctx, request.Params{
		Method: http.MethodPost,
		URL:    "https://api.telegram.org/bot" + e.tgToken + "/setWebhook",
		Body: map[string]string{
			"url":          u.String(),
			"secret_token": e.tgSecret,
		},
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
		HTTPClient: e.httpc,
		Scrubber:   e.scrubber,
	})
	return err
}

// selfPing continusly pings Starlet every 10 minutes in production to prevent it's Render app from sleeping.
func (e *engine) selfPing(ctx context.Context, getenv func(string) string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	e.logf("selfPing: started")
	defer e.logf("selfPing: stopped")

	for {
		select {
		case <-ticker.C:
			url := getenv("RENDER_EXTERNAL_URL")
			if url == "" {
				e.logf("selfPing: RENDER_EXTERNAL_URL is not set; are you really on Render?")
				return
			}
			health, err := request.Make[web.HealthResponse](ctx, request.Params{
				Method: http.MethodGet,
				URL:    url + "/health",
				Headers: map[string]string{
					"User-Agent": version.UserAgent(),
				},
				HTTPClient: e.httpc,
				Scrubber:   e.scrubber,
			})
			if err != nil {
				e.logf("selfPing: %v", err)
			}
			if !health.OK {
				e.logf("selfPing: unhealthy: %+v", health)
			}
		case <-ctx.Done():
			return
		}
	}
}

// }}}

// Error handling {{{

// https://core.telegram.org/bots/api#linkpreviewoptions
type linkPreviewOptions struct {
	IsDisabled bool `json:"is_disabled"`
}

// https://core.telegram.org/bots/api#message
type message struct {
	tgmarkup.Message
	ChatID             int64              `json:"chat_id"`
	LinkPreviewOptions linkPreviewOptions `json:"link_preview_options"`
}

// e.mu must be held for reading.
func (e *engine) reportError(ctx context.Context, chatID int64, w http.ResponseWriter, err error) {
	errMsg := err.Error()
	if evalErr, ok := err.(*starlark.EvalError); ok {
		errMsg = evalErr.Backtrace()
	}
	if e.scrubber != nil {
		// Mask secrets in error messages.
		errMsg = e.scrubber.Replace(errMsg)
	}

	msg := message{
		ChatID: chatID,
		LinkPreviewOptions: linkPreviewOptions{
			IsDisabled: true,
		},
	}

	errTmpl := e.errorTemplate
	if e.errorTemplate == "" {
		errTmpl = defaultErrorTemplate
	}
	msg.Message = tgmarkup.FromMarkdown(fmt.Sprintf(errTmpl, errMsg))

	_, sendErr := request.Make[any](ctx, request.Params{
		Method:     http.MethodPost,
		URL:        "https://api.telegram.org/bot" + e.tgToken + "/sendMessage",
		Body:       msg,
		HTTPClient: e.httpc,
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
		Scrubber: e.scrubber,
	})
	if sendErr != nil {
		e.logf("Reporting an error %q to bot owner (%q) failed: %v", err, e.tgOwner, sendErr)
	}

	// Don't respond with an error because Telegram will go mad and start retrying
	// updates until my Telegram chat is fucked with lots of error messages.
	jsonOK(w)
}

// }}}

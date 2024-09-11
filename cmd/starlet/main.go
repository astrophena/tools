// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// vim: foldmethod=marker

/*
Starlet is a Telegram bot runner using Starlark.

Starlet acts as an intermediary between the Telegram Bot API and Starlark
code, enabling the creation of Telegram bots using the Starlark scripting
language. It provides a simple way to define bot commands, handle incoming
messages, and interact with the Telegram API.

Starlet periodically pings itself to prevent [Render] from putting it to
sleep, ensuring continuous operation.

# Starlark environment

In addition to the standard Starlark modules, the following modules are
available to the bot code:

	config: Contains bot configuration.
		- owner_id (int): Telegram user ID of the bot owner.
		- version (str): Bot version string.

	convcache: Allows to cache bot conversations.
		- get(chat_id: int) -> list: Retrieves the conversation history for the given chat ID.
		- append(chat_id: int, message: str): Appends a new message to the conversation history.
		- reset(chat_id: int): Clears the conversation history for the given chat ID.

	files: Allows to retrieve files from GitHub Gist with bot code.
		- read(name: str) -> str: Retrieves a file from GitHub Gist.

	gemini: Allows interaction with Gemini API.
		- generate_content(contents, system, unsafe): Generates text using Gemini:
			- contents (list of strings): The text to be provided to Gemini for generation.
			- system (dict, optional): System instructions to guide Gemini's response, containing a single key "text" with string value.
			- unsafe (bool, optional): Disables all model safety measures.

	markdown: Allows operations with Markdown text.
		- strip(text: str) -> str: Strips out all formatting from Markdown text.

	html: Helper functions for working with HTML.
		- escape(s): Escapes HTML string.

	telegram: Allows sending requests to the Telegram Bot API.
		- call(method, args): Calls a Telegram Bot API method:
			- method (string): The Telegram Bot API method to call.
			- args (dict): The arguments to pass to the method.

	time: Provides time-related functions.

See https://pkg.go.dev/go.starlark.net/lib/time#Module for documentation of
the time module.

# GitHub Gist structure

The GitHub Gist containing the bot code must have the following structure:

  - bot.star: Contains the Starlark code for the bot.
  - error.tmpl (optional): Contains the HTML template for error messages. If omitted,
    a default template will be used. The template receives the error message as %v.

# Entry point

The bot code must define a function called handle that takes a single
argument — a dictionary representing the Telegram update. This function is
called by Starlet for each incoming update.

If you define a function on_load, it will be called by Starlet each time it
loads bot code from GitHub Gist. This can be used, for example, to update
command list in Telegram.

# Environment variables

The following environment variables can be used to configure Starlet:

  - GEMINI_KEY: Gemini API key.
  - GH_TOKEN: GitHub API token.
  - GIST_ID: GitHub Gist ID to load bot code from.
  - HOST: Bot domain used for setting up webhook.
  - TG_OWNER: Telegram user ID of the bot owner.
  - TG_SECRET: Secret token used to validate Telegram Bot API updates.
  - TG_TOKEN: Telegram Bot API token.

# Debug interface

Starlet provides a debug interface at /debug with the following endpoints:

  - /debug/code: Displays the currently loaded bot code.
  - /debug/logs: Displays the last 300 lines of logs, streamed automatically.
  - /debug/reload: Reloads the bot code from the GitHub Gist.

Authentication through Telegram is required to access the debug interface
when running on Render. The user must be the bot owner to successfully
authenticate.

See https://core.telegram.org/widgets/login for guidance. Use "https://<bot
URL>/login" as login URL.

[Render]: https://render.com
*/
package main

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/request"
	"go.astrophena.name/tools/cmd/starlet/internal/convcache"
	"go.astrophena.name/tools/cmd/starlet/internal/starlarkconv"
	"go.astrophena.name/tools/cmd/starlet/internal/starlarkgemini"
	"go.astrophena.name/tools/cmd/starlet/internal/telegram"
	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/api/google/gemini"
	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/util/logstream"
	"go.astrophena.name/tools/internal/version"
	"go.astrophena.name/tools/internal/web"

	stripmd "github.com/writeas/go-strip-markdown/v2"
	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

const defaultErrorTemplate = `❌ Something went wrong:
<pre><code>%v</code></pre>`

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	e := new(engine)
	cli.Run(e.main(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func (e *engine) main(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	// Define and parse flags.
	a := &cli.App{
		Name:        "starlet",
		Description: helpDoc,
		Flags:       flag.NewFlagSet("starlet", flag.ContinueOnError),
	}
	var (
		addr      = a.Flags.String("addr", "localhost:3000", "Listen on `host:port`.")
		geminiKey = a.Flags.String("gemini-key", "", "Gemini API `key`.")
		ghToken   = a.Flags.String("gh-token", "", "GitHub API `token`.")
		gistID    = a.Flags.String("gist-id", "", "GitHub Gist `ID` to load bot code from.")
		host      = a.Flags.String("host", "", "Bot `domain` used for setting up webhook.")
		tgOwner   = a.Flags.Int64("tg-owner", 0, "Telegram user `ID` of the bot owner.")
		tgSecret  = a.Flags.String("tg-secret", "", "Secret `token` used to validate Telegram Bot API updates.")
		tgToken   = a.Flags.String("tg-token", "", "Telegram Bot API `token`.")
	)
	if err := a.HandleStartup(args, stdout, stderr); err != nil {
		if errors.Is(err, cli.ErrExitVersion) {
			return nil
		}
		return err
	}

	// Load configuration from environment variables or flags.
	e.geminiKey = cmp.Or(e.geminiKey, getenv("GEMINI_KEY"), *geminiKey)
	e.ghToken = cmp.Or(e.ghToken, getenv("GH_TOKEN"), *ghToken)
	e.gistID = cmp.Or(e.gistID, getenv("GIST_ID"), *gistID)
	e.host = cmp.Or(e.host, getenv("HOST"), *host)
	e.tgOwner = cmp.Or(e.tgOwner, parseInt(getenv("TG_OWNER")), *tgOwner)
	e.tgSecret = cmp.Or(e.tgSecret, getenv("TG_SECRET"), *tgSecret)
	e.tgToken = cmp.Or(e.tgToken, getenv("TG_TOKEN"), *tgToken)
	e.onRender = getenv("RENDER") == "true"

	// Initialize internal state.
	e.stderr = stderr
	e.init.Do(e.doInit)

	// Used in tests.
	if e.noServerStart {
		return nil
	}

	// If running on Render, try to look up port to listen on, activate webhook
	// and start goroutine that prevents Starlet from sleeping.
	if e.onRender {
		// https://docs.render.com/environment-variables#all-runtimes-1
		if port := getenv("PORT"); port != "" {
			*addr = ":" + port
		}
		if err := e.setWebhook(ctx); err != nil {
			return err
		}
		go e.selfPing(ctx)
	}

	return web.ListenAndServe(ctx, &web.ListenAndServeConfig{
		Addr:       *addr,
		DebugAuth:  e.debugAuth,
		Debuggable: true, // debug endpoints protected by Telegram auth
		Logf:       e.logf,
		Mux:        e.mux,
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
	init     sync.Once // main initialization
	loadGist sync.Once // lazily loads gist when first webhook request arrives

	// initialized by doInit
	convCache *starlarkstruct.Module
	geminic   *gemini.Client
	gistc     *gist.Client
	scrubber  *strings.Replacer
	logStream logstream.Streamer
	logf      logger.Logf
	mux       *http.ServeMux

	// test flags
	noServerStart bool

	// configuration, read-only after initialization
	geminiKey string
	ghToken   string
	gistID    string
	host      string
	httpc     *http.Client
	onRender  bool
	stderr    io.Writer
	tgOwner   int64
	tgSecret  string
	tgToken   string

	mu sync.Mutex
	// loaded from gist
	bot           []byte
	botProg       starlark.StringDict
	files         map[string]gist.File
	errorTemplate string
	loadGistErr   error
}

func (e *engine) doInit() {
	if e.httpc == nil {
		e.httpc = &http.Client{
			Timeout: 10 * time.Second,
		}
	}
	if e.stderr == nil {
		e.stderr = os.Stderr
	}

	const logLineLimit = 300
	e.logStream = logstream.New(logLineLimit)
	e.logf = log.New(io.MultiWriter(e.stderr, e.logStream), "", log.LstdFlags).Printf
	e.initRoutes()

	scrubPairs := []string{
		e.ghToken, "[EXPUNGED]",
		e.gistID, "[EXPUNGED]",
		e.tgSecret, "[EXPUNGED]",
		e.tgToken, "[EXPUNGED]",
	}
	if e.geminiKey != "" {
		scrubPairs = append(scrubPairs, e.geminiKey, "[EXPUNGED]")
	}

	// Quick sanity check.
	if len(scrubPairs)%2 != 0 {
		panic("scrubPairs are not even; check doInit method on engine")
	}

	e.convCache = convcache.Module(24 * time.Hour)

	e.scrubber = strings.NewReplacer(scrubPairs...)

	e.gistc = &gist.Client{
		Token:      e.ghToken,
		HTTPClient: e.httpc,
		Scrubber:   e.scrubber,
	}
	if e.geminiKey != "" {
		e.geminic = &gemini.Client{
			APIKey:     e.geminiKey,
			Model:      "gemini-1.5-flash-latest",
			HTTPClient: e.httpc,
			Scrubber:   e.scrubber,
		}
	}
}

func (e *engine) initRoutes() {
	e.mux = http.NewServeMux()

	// Redirect to Telegram chat with bot.
	e.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			e.respondError(w, web.ErrNotFound)
			return
		}
		http.Redirect(w, r, "https://t.me/astrophena_bot", http.StatusFound)
	})

	e.mux.HandleFunc("POST /telegram", e.handleTelegramWebhook)

	// Redirect from starlet.onrender.com to bot.astrophena.name.
	if e.onRender {
		e.mux.HandleFunc("starlet.onrender.com/", func(w http.ResponseWriter, r *http.Request) {
			targetURL := "https://bot.astrophena.name" + r.URL.Path
			http.Redirect(w, r, targetURL, http.StatusMovedPermanently)
		})
	}

	// Authentication.
	e.mux.HandleFunc("GET /login", e.handleLogin)

	// Debug routes.
	web.Health(e.mux)
	dbg := web.Debugger(e.logf, e.mux)

	dbg.HandleFunc("code", "Bot code", func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.bot == nil {
			fmt.Fprintf(w, "(not loaded, go to /debug/reload or talk with bot on Telegram)\n")
			return
		}
		w.Write(e.bot)
	})

	dbg.HandleFunc("logs", "Logs", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, logsTmpl, strings.Join(e.logStream.Lines(), ""))
	})
	e.mux.Handle("/debug/log", e.logStream)

	dbg.HandleFunc("reload", "Reload from gist", func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.loadFromGist(r.Context())
		err := e.loadGistErr
		if err != nil {
			e.respondError(w, err)
			return
		}
		http.Redirect(w, r, "/debug/", http.StatusFound)
	})
}

func jsonOK(w http.ResponseWriter) {
	var res struct {
		Status string `json:"success"`
	}
	res.Status = "success"
	web.RespondJSON(w, res)
}

const logsTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="/style.css">
<title>Logs</title>
<script>
const maxLines = 300;
new EventSource("/debug/log", { withCredentials: true }).addEventListener("logline", function(e) {
  // Append line to whatever is in the pre block. Then, truncate number of lines to maxLines.
  // This is extremely inefficient, since we're splitting into component lines and joining them
  // back each time a line is added.
  var txt = document.getElementById("logs").innerText + e.data + "\n";
  document.getElementById("logs").innerText = txt.split('\n').slice(-maxLines).join('\n');
});
</script>
</head>
<body>
<main>
<h1>Logs</h1>
<p><i>The last 300 lines are displayed, and new ones are streamed automatically.</i></p>
<pre><code id="logs">%[1]s</code></pre>
</main>
</body>
</html>`

// e.mu must be held.
func (e *engine) loadFromGist(ctx context.Context) {
	g, err := e.gistc.Get(ctx, e.gistID)
	if err != nil {
		e.loadGistErr = err
		return
	}

	bot, exists := g.Files["bot.star"]
	if !exists {
		e.loadGistErr = errors.New("bot.star should contain bot code in Gist")
		return
	}
	botCode := []byte(bot.Content)
	if errorTmpl, exists := g.Files["error.tmpl"]; exists {
		e.errorTemplate = errorTmpl.Content
	} else {
		e.errorTemplate = defaultErrorTemplate
	}

	predeclared := starlark.StringDict{
		"config": starlarkstruct.FromStringDict(
			starlarkstruct.Default,
			starlark.StringDict{
				"owner_id": starlark.MakeInt64(e.tgOwner),
				"version":  starlark.String(version.Version().String()),
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
				"strip": starlark.NewBuiltin("markdown.strip", stripMarkdown),
			},
		},
		"html": &starlarkstruct.Module{
			Name: "html",
			Members: starlark.StringDict{
				"escape": starlark.NewBuiltin("html.escape", escapeHTML),
			},
		},
		"telegram": telegram.Module(e.tgToken, e.httpc),
		"time":     starlarktime.Module,
	}

	botProg, err := starlark.ExecFileOptions(
		&syntax.FileOptions{},
		e.newStarlarkThread(context.Background()),
		"bot.star",
		botCode,
		predeclared,
	)
	if err != nil {
		e.loadGistErr = err
		return
	}

	if hook, ok := botProg["on_load"]; ok {
		_, err = starlark.Call(e.newStarlarkThread(ctx), hook, starlark.Tuple{}, nil)
		if err != nil {
			e.loadGistErr = err
			return
		}
	}

	e.bot = botCode
	e.botProg = botProg
	e.files = g.Files
	e.loadGistErr = nil
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
	jsonErr := func(err error) { web.RespondJSONError(e.logf, w, err) }

	if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != e.tgSecret {
		jsonErr(web.ErrNotFound)
		return
	}

	b, err := io.ReadAll(r.Body)
	if err != nil {
		jsonErr(err)
		return
	}
	var gu map[string]any
	if err := json.Unmarshal(b, &gu); err != nil {
		jsonErr(err)
		return
	}
	u, err := starlarkconv.ToValue(gu)
	if err != nil {
		jsonErr(err)
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.loadGist.Do(func() { e.loadFromGist(r.Context()) })
	if e.loadGistErr != nil {
		e.reportError(r.Context(), w, e.loadGistErr)
		jsonOK(w)
		return
	}

	f, ok := e.botProg["handle"]
	if !ok {
		e.reportError(r.Context(), w, errNoHandleFunc)
		return
	}

	_, err = starlark.Call(e.newStarlarkThread(r.Context()), f, starlark.Tuple{u}, nil)
	if err != nil {
		e.reportError(r.Context(), w, err)
		return
	}

	jsonOK(w)
}

func escapeHTML(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "s", &s); err != nil {
		return starlark.None, err
	}
	return starlark.String(html.EscapeString(s)), nil
}

func stripMarkdown(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "s", &s); err != nil {
		return starlark.None, err
	}
	return starlark.String(stripmd.Strip(s)), nil
}

// e.mu must be held.
func (e *engine) readFile(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name", &name); err != nil {
		return starlark.None, err
	}
	file, exists := e.files[name]
	if !exists {
		return starlark.None, fmt.Errorf("file %s not found in Gist", name)
	}
	return starlark.String(file.Content), nil
}

func (e *engine) setWebhook(ctx context.Context) error {
	if e.host == "" {
		return errors.New("host hasn't set; pass it with -host flag or HOST environment variable")
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

// Error handling {{{

func (e *engine) respondError(w http.ResponseWriter, err error) {
	web.RespondError(e.logf, w, err)
}

// e.mu must be held.
func (e *engine) reportError(ctx context.Context, w http.ResponseWriter, err error) {
	errMsg := err.Error()
	if evalErr, ok := err.(*starlark.EvalError); ok {
		errMsg = evalErr.Backtrace()
	}
	// Mask secrets in error messages.
	errMsg = e.scrubber.Replace(errMsg)

	// https://core.telegram.org/bots/api#linkpreviewoptions
	type linkPreviewOptions struct {
		IsDisabled bool `json:"is_disabled"`
	}

	_, sendErr := request.Make[any](ctx, request.Params{
		Method: http.MethodPost,
		URL:    "https://api.telegram.org/bot" + e.tgToken + "/sendMessage",
		Body: map[string]any{
			"chat_id":    strconv.FormatInt(e.tgOwner, 10),
			"text":       fmt.Sprintf(e.errorTemplate, html.EscapeString(errMsg)),
			"parse_mode": "HTML",
			"link_preview_options": linkPreviewOptions{
				IsDisabled: true,
			},
		},
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

// Authentication {{{

func (e *engine) debugAuth(r *http.Request) bool {
	if !e.onRender || e.loggedIn(r) {
		return true
	}
	return false
}

func (e *engine) handleLogin(w http.ResponseWriter, r *http.Request) {
	// See https://core.telegram.org/widgets/login#receiving-authorization-data.
	data := r.URL.Query()
	hash := data.Get("hash")
	if hash == "" {
		e.respondError(w, fmt.Errorf("%w: no hash present in auth data", web.ErrBadRequest))
		return
	}
	data.Del("hash")

	var keys []string
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		sb.WriteString(k + "=" + data.Get(k))
		// Don't append newline on last key.
		if i+1 != len(keys) {
			sb.WriteString("\n")
		}
	}
	checkString := sb.String()

	if !e.validateAuthData(checkString, hash) {
		e.respondError(w, fmt.Errorf("%w: hash is not valid", web.ErrBadRequest))
		return
	}

	setCookie(w, "auth_data", base64.URLEncoding.EncodeToString([]byte(checkString)))
	setCookie(w, "auth_data_hash", hash)

	http.Redirect(w, r, "/debug/", http.StatusFound)
}

func (e *engine) loggedIn(r *http.Request) bool {
	if len(r.Cookies()) == 0 {
		return false
	}
	var data, hash string
	for _, cookie := range r.Cookies() {
		switch cookie.Name {
		case "auth_data":
			bdata, err := base64.URLEncoding.DecodeString(cookie.Value)
			if err != nil {
				return false
			}
			data = string(bdata)
		case "auth_data_hash":
			hash = cookie.Value
		}
	}
	if !e.validateAuthData(data, hash) {
		return false
	}

	dataMap := extractAuthData(data)

	// Check if ID of authenticated user matches the bot owner ID.
	if dataMap["id"] != strconv.FormatInt(e.tgOwner, 10) {
		return false
	}
	// Check if auth data was not created more that 24 hours ago.
	authDateUnix, err := strconv.ParseInt(dataMap["auth_date"], 0, 64)
	if err != nil {
		return false
	}
	return time.Since(time.Unix(authDateUnix, 0)) < 24*time.Hour
}

func extractAuthData(data string) map[string]string {
	dataMap := make(map[string]string)
	for _, line := range strings.Split(data, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			dataMap[parts[0]] = parts[1]
		}
	}
	return dataMap
}

func (e *engine) validateAuthData(data, hash string) bool {
	// Compute SHA256 hash of the token, serving as the secret key for HMAC.
	h := sha256.New()
	h.Write([]byte(e.tgToken))
	tokenHash := h.Sum(nil)

	// Compute HMAC signature of authentication data.
	hm := hmac.New(sha256.New, tokenHash)
	hm.Write([]byte(data))
	gotHash := hex.EncodeToString(hm.Sum(nil))

	return gotHash == hash
}

func setCookie(w http.ResponseWriter, key, val string) {
	http.SetCookie(w, &http.Cookie{
		Name:     key,
		Value:    val,
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
	})
}

// }}}

// selfPing continusly pings Starlet every 10 minutes in production to prevent it's Render app from sleeping.
func (e *engine) selfPing(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	e.logf("selfPing: started")
	defer e.logf("selfPing: stopped")

	for {
		select {
		case <-ticker.C:
			url := os.Getenv("RENDER_EXTERNAL_URL")
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

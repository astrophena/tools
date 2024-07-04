// vim: foldmethod=marker

/*
Starlet allows to create and manage a Telegram bot using the Starlark scripting language.

Starlet serves an HTTP server to handle Telegram webhook updates and bot
maintenance. The bot's code is sourced from a specified GitHub Gist. Also
Starlet includes features such as user authentication via the Telegram login
widget, ensuring that only the designated bot owner can manage the bot. In
production environments, Starlet periodically pings itself to prevent the
hosting service from putting it to sleep, ensuring continuous operation.

# Starlark language

See [Starlark spec] for reference.

Additional modules and functions available from bot code:

  - call: Make HTTP POST requests to the Telegram API,
    facilitating bot commands and interactions.
  - escape_html: Escape HTML string.
  - json: The Starlark JSON module, enabling JSON parsing and encoding.
  - time: The Starlark time module, providing time-related functions.

[Starlark spec]: https://github.com/bazelbuild/starlark/blob/master/spec.md
*/
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.astrophena.name/tools/internal/api/gist"
	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/cli/envflag"
	"go.astrophena.name/tools/internal/logger"
	"go.astrophena.name/tools/internal/request"
	"go.astrophena.name/tools/internal/version"
	"go.astrophena.name/tools/internal/web"

	starlarkjson "go.starlark.net/lib/json"
	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

//go:embed icon.png
var debugIcon []byte

const defaultErrorTemplate = `❌ Something went wrong:
<pre><code>%v</code></pre>`

func main() {
	var (
		addr = envflag.Value(
			"addr", "ADDR", "localhost:3000",
			"Listen on `host:port`.",
		)
		tgToken = envflag.Value(
			"tg-token", "TG_TOKEN", "",
			"Telegram Bot API `token`.",
		)
		tgSecret = envflag.Value(
			"tg-secret", "TG_SECRET", "",
			"Secret `token` used to validate Telegram Bot API updates.",
		)
		tgOwner = envflag.Value(
			"tg-owner", "TG_OWNER", int64(0),
			"Telegram user `ID` of the bot owner.",
		)
		ghToken = envflag.Value(
			"gh-token", "GH_TOKEN", "",
			"GitHub API `token`.",
		)
		gistID = envflag.Value(
			"gist-id", "GIST_ID", "",
			"GitHub Gist `ID` to load bot code from.",
		)
		reloadToken = envflag.Value(
			"reload-token", "RELOAD_TOKEN", "",
			"Token that can be used to authenticate /debug/reload requests. This can be used in Git hook, for example.",
		)
	)
	cli.HandleStartup()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	e := &engine{
		tgToken:     *tgToken,
		tgSecret:    *tgSecret,
		tgOwner:     *tgOwner,
		ghToken:     *ghToken,
		gistID:      *gistID,
		reloadToken: *reloadToken,
		httpc: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	e.init.Do(e.doInit)

	if isProd() {
		// https://docs.render.com/environment-variables#all-runtimes-1
		if port := os.Getenv("PORT"); port != "" {
			*addr = ":" + port
		}
		go e.selfPing(ctx)
	}

	if err := web.ListenAndServe(ctx, &web.ListenAndServeConfig{
		Addr:       *addr,
		DebugAuth:  e.debugAuth,
		Debuggable: true, // debug endpoints protected by Telegram auth
		Logf:       e.logf,
		Mux:        e.mux,
	}); err != nil {
		log.Fatal(err)
	}
}

// https://docs.render.com/environment-variables
func isProd() bool { return os.Getenv("RENDER") == "true" }

type engine struct {
	init     sync.Once // main initialization
	loadGist sync.Once // lazily loads gist when first webhook request arrives

	// initialized by doInit
	gistc     *gist.Client
	log       *log.Logger
	logStream logger.Streamer
	mux       *http.ServeMux
	logMasker *strings.Replacer

	// configuration, read-only after initialization
	ghToken     string
	gistID      string
	httpc       *http.Client
	tgOwner     int64
	tgSecret    string
	tgToken     string
	reloadToken string

	mu sync.Mutex
	// loaded from gist
	bot           []byte
	errorTemplate string
	loadGistErr   error
}

func (e *engine) doInit() {
	const logLineLimit = 300
	e.logStream = logger.NewStreamer(logLineLimit)
	e.log = log.New(io.MultiWriter(os.Stderr, e.logStream), "", log.LstdFlags)
	e.initRoutes()

	e.logMasker = strings.NewReplacer(
		e.tgToken, "[EXPUNGED]",
		e.tgSecret, "[EXPUNGED]",
		e.ghToken, "[EXPUNGED]",
		e.gistID, "[EXPUNGED]",
	)

	e.gistc = &gist.Client{
		Token:      e.ghToken,
		HTTPClient: e.httpc,
		Scrubber:   e.logMasker,
	}
}

func (e *engine) logf(format string, args ...any) { e.log.Printf(format, args...) }

func (e *engine) initRoutes() {
	e.mux = http.NewServeMux()

	// Redirect to Telegram chat with bot.
	e.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			web.RespondError(e.logf, w, web.ErrNotFound)
			return
		}
		http.Redirect(w, r, "https://t.me/astrophena_bot", http.StatusFound)
	})

	e.mux.HandleFunc("POST /telegram", e.handleTelegramWebhook)

	// Redirect from starlet.onrender.com to bot.astrophena.name.
	if isProd() {
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
	dbg.SetIcon(debugIcon)
	dbg.HandleFunc("reload", "Reload from gist", func(w http.ResponseWriter, r *http.Request) {
		e.loadFromGist(r.Context())
		e.mu.Lock()
		defer e.mu.Unlock()
		err := e.loadGistErr
		if err != nil {
			web.RespondError(e.logf, w, err)
			return
		}
		http.Redirect(w, r, "/debug/", http.StatusFound)
	})
	dbg.HandleFunc("logs", "Logs", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, logsTmpl, strings.Join(e.logStream.Lines(), ""))
	})
	e.mux.Handle("/debug/log", e.logStream)
	dbg.HandleFunc("code", "Bot code", func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.bot == nil {
			fmt.Fprintf(w, "(not loaded, go to /debug/reload or talk with bot on Telegram)\n")
			return
		}
		w.Write(e.bot)
	})
	dbg.HandleFunc("version", "Version (JSON)", func(w http.ResponseWriter, r *http.Request) {
		web.RespondJSON(w, version.Version())
	})
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

func (e *engine) loadFromGist(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()

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
	e.bot = []byte(bot.Content)
	if errorTmpl, exists := g.Files["error.tmpl"]; exists {
		e.errorTemplate = errorTmpl.Content
	} else {
		e.errorTemplate = defaultErrorTemplate
	}
	e.loadGistErr = nil
}

func (e *engine) handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != e.tgSecret {
		web.RespondJSONError(e.logf, w, web.ErrNotFound)
		return
	}

	rawUpdate, err := io.ReadAll(r.Body)
	if err != nil {
		web.RespondJSONError(e.logf, w, err)
		return
	}

	predeclared := starlark.StringDict{
		"bot_owner_id": starlark.MakeInt64(e.tgOwner),
		"call":         starlark.NewBuiltin("call", e.callFunc(r.Context())),
		"escape_html":  starlark.NewBuiltin("escape_html", escapeHTML),
		"json":         starlarkjson.Module,
		"raw_update":   starlark.String(rawUpdate),
		"time":         starlarktime.Module,
	}

	e.loadGist.Do(func() { e.loadFromGist(r.Context()) })
	e.mu.Lock()
	var (
		gistErr = e.loadGistErr
		botCode = bytes.Clone(e.bot)
	)
	e.mu.Unlock()

	if gistErr != nil {
		web.RespondJSONError(e.logf, w, e.loadGistErr)
		return
	}

	_, err = starlark.ExecFileOptions(&syntax.FileOptions{}, &starlark.Thread{
		Print: func(thread *starlark.Thread, msg string) { e.log.Println(msg) },
	}, "bot.star", botCode, predeclared)
	if err != nil {
		e.reportError(r.Context(), w, err)
		return
	}

	jsonOK(w)
}

func jsonOK(w http.ResponseWriter) {
	var res struct {
		Status string `json:"success"`
	}
	res.Status = "success"
	web.RespondJSON(w, res)
}

// Authentication {{{

func (e *engine) debugAuth(r *http.Request) bool {
	if !isProd() || e.loggedIn(r) {
		return true
	}
	if r.URL.Path == "/debug/reload" {
		return r.Header.Get("X-Reload-Token") == e.reloadToken
	}
	return false
}

func (e *engine) handleLogin(w http.ResponseWriter, r *http.Request) {
	// See https://core.telegram.org/widgets/login#receiving-authorization-data.
	data := r.URL.Query()
	hash := data.Get("hash")
	if hash == "" {
		web.RespondError(e.logf, w, fmt.Errorf("%w: no hash present in auth data", web.ErrBadRequest))
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
		web.RespondError(e.logf, w, fmt.Errorf("%w: hash is not valid", web.ErrBadRequest))
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
			health, err := request.MakeJSON[web.HealthResponse](ctx, request.Params{
				Method:     http.MethodGet,
				URL:        url + "/health",
				HTTPClient: e.httpc,
				Scrubber:   e.logMasker,
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

func (e *engine) reportError(ctx context.Context, w http.ResponseWriter, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	errMsg := err.Error()
	if evalErr, ok := err.(*starlark.EvalError); ok {
		errMsg += "\n\n" + evalErr.Backtrace()
	}
	// Mask secrets in error messages.
	errMsg = e.logMasker.Replace(errMsg)

	_, sendErr := request.MakeJSON[any](ctx, request.Params{
		Method: http.MethodPost,
		URL:    "https://api.telegram.org/bot" + e.tgToken + "/sendMessage",
		Body: map[string]string{
			"chat_id":    strconv.FormatInt(e.tgOwner, 10),
			"text":       fmt.Sprintf(e.errorTemplate, html.EscapeString(errMsg)),
			"parse_mode": "HTML",
		},
		HTTPClient: e.httpc,
		Scrubber:   e.logMasker,
	})
	if sendErr != nil {
		e.logf("reporting an error %q to %q failed: %v", err, e.tgOwner, sendErr)
	}

	web.RespondJSONError(e.logf, w, err)
}

type starlarkBuiltin func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error)

func escapeHTML(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "s", &s); err != nil {
		return nil, err
	}
	return starlark.String(html.EscapeString(s)), nil
}

/*
callFunc implements a Starlark builtin that makes request to Telegram Bot API and returns it's response.

It accepts two keyword arguments: method (string, Telegram Bot API method to call) and args (dict, arguments
for this method).

For example, to send a message, you can write a function like this:

	def send_message(to, text, reply_markup = {}):
	    return call(
	        method = "sendMessage",
	        args = {
	            "chat_id": to,
	            "text": text,
	            "reply_markup": reply_markup
	        }
	    )
*/
func (e *engine) callFunc(ctx context.Context) starlarkBuiltin {
	return func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		// Unpack arguments passed from Starlark code.
		if len(args) > 0 {
			return starlark.None, fmt.Errorf("%s: unexpected positional arguments", b.Name())
		}
		var (
			method   starlark.String
			argsDict *starlark.Dict
		)
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "method", &method, "args", &argsDict); err != nil {
			return nil, err
		}

		// Encode received args to JSON.
		rawReqVal, err := starlark.Call(thread, starlarkjson.Module.Members["encode"], starlark.Tuple{argsDict}, []starlark.Tuple{})
		if err != nil {
			return nil, fmt.Errorf("%s: failed to encode received args to JSON: %v", b.Name(), err)
		}
		rawReq, ok := rawReqVal.(starlark.String)
		if !ok {
			panic(fmt.Sprintf("%s: unexpected return type of json.encode Starlark function", b.Name()))
		}

		// Make Telegram Bot API request.
		rawResp, err := request.MakeJSON[json.RawMessage](ctx, request.Params{
			Method:     http.MethodPost,
			URL:        "https://api.telegram.org/bot" + e.tgToken + "/" + string(method),
			Body:       json.RawMessage(rawReq),
			HTTPClient: e.httpc,
			Scrubber:   e.logMasker,
		})
		if err != nil {
			return nil, fmt.Errorf("%s: failed to make request: %s", b.Name(), err)
		}

		// Decode received JSON returned from Telegram and pass it back to Starlark code.
		return starlark.Call(thread, starlarkjson.Module.Members["decode"], starlark.Tuple{starlark.String(rawResp)}, []starlark.Tuple{})
	}
}

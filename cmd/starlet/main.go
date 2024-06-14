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
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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

	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/client/gist"
	"go.astrophena.name/tools/internal/httputil"
	"go.astrophena.name/tools/internal/version"
	"go.astrophena.name/tools/internal/web"

	starlarkjson "go.starlark.net/lib/json"
	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
)

func main() {
	addr := flag.String(
		"addr", "localhost:3000",
		"Listen on `host:port or Unix socket`. Can be overridden by PORT environment variable.",
	)
	tgToken := flag.String(
		"tg-token", "",
		"Telegram Bot API `token`. Can be overridden by TG_TOKEN environment variable.",
	)
	tgSecret := flag.String(
		"tg-secret", "",
		"Secret `token` used to validate Telegram Bot API updates. Can be overridden by TG_SECRET environment variable.",
	)
	tgOwner := flag.Int64(
		"tg-owner", 0,
		"Telegram user `ID` of the bot owner. Can be overridden by TG_OWNER environment variable.",
	)
	ghToken := flag.String(
		"gh-token", "",
		"GitHub API `token`. Can be overridden by GH_TOKEN environment variable.",
	)
	gistID := flag.String(
		"gist-id", "",
		"GitHub Gist `ID` to load bot code from. Can be overriden by GIST_ID environment variable.",
	)
	cli.HandleStartup()

	if port := os.Getenv("PORT"); port != "" {
		*addr = "0.0.0.0:" + port
	}
	if tok := os.Getenv("TG_TOKEN"); tok != "" {
		*tgToken = tok
	}
	if secret := os.Getenv("TG_SECRET"); secret != "" {
		*tgSecret = secret
	}
	if owner := os.Getenv("TG_OWNER"); owner != "" {
		newOwner, err := strconv.ParseInt(owner, 10, 64)
		if err == nil {
			*tgOwner = newOwner
		}
	}
	if tok := os.Getenv("GH_TOKEN"); tok != "" {
		*ghToken = tok
	}
	if id := os.Getenv("GIST_ID"); id != "" {
		*gistID = id
	}

	httpc := &http.Client{
		Timeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	e := &engine{
		tgToken:  *tgToken,
		tgSecret: *tgSecret,
		tgOwner:  *tgOwner,
		gistc: &gist.Client{
			Token:      *ghToken,
			HTTPClient: httpc,
		},
		gistID: *gistID,
		httpc:  httpc,
		mux:    http.NewServeMux(),
	}

	if err := e.loadFromGist(ctx); err != nil {
		log.Fatalf("loadFromGist: %v", err)
	}
	web.Debugger(e.mux).Handle("reload", "Reload from gist", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := e.loadFromGist(ctx); err != nil {
			web.Error(w, r, err)
		}
		http.Redirect(w, r, "/debug/", http.StatusFound)
	}))

	e.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			web.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "https://t.me/astrophena_bot", http.StatusFound)
	})
	if isProd() {
		e.mux.HandleFunc("starlet.onrender.com/", func(w http.ResponseWriter, r *http.Request) {
			targetURL := "https://bot.astrophena.name" + r.URL.Path
			http.Redirect(w, r, targetURL, http.StatusMovedPermanently)
		})
	}
	e.mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, version.Version().Short())
	})
	e.mux.HandleFunc("GET /login", e.handleLogin)
	e.mux.HandleFunc("POST /telegram", e.handleTelegramWebhook)

	if isProd() {
		go e.selfPing(ctx)
	}

	web.ListenAndServe(ctx, &web.ListenAndServeConfig{
		Mux:        e.mux,
		Addr:       *addr,
		Debuggable: true, // debug endpoints protected by Telegram auth
		DebugAuth:  e.debugAuth,
	})
}

func (e *engine) debugAuth(r *http.Request) bool {
	if !isProd() || e.loggedIn(r) {
		return true
	}
	return false
}

func (e *engine) loadFromGist(ctx context.Context) error {
	g, err := e.gistc.Get(ctx, e.gistID)
	if err != nil {
		return err
	}

	bot, exists := g.Files["bot.star"]
	if !exists {
		return errors.New("bot.star should contain bot code in Gist")
	}

	e.mu.Lock()
	e.bot = []byte(bot.Content)
	e.mu.Unlock()

	return nil
}

// https://docs.render.com/environment-variables
func isProd() bool { return os.Getenv("RENDER") == "true" }

type engine struct {
	// configuration, read-only
	gistID   string
	tgOwner  int64
	tgSecret string
	tgToken  string

	gistc *gist.Client
	httpc *http.Client
	mux   *http.ServeMux

	mu  sync.Mutex
	bot []byte
}

func (e *engine) handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != e.tgSecret {
		web.NotFound(w, r)
		return
	}

	rawUpdate, err := io.ReadAll(r.Body)
	if err != nil {
		web.Error(w, r, err)
		return
	}

	predeclared := starlark.StringDict{
		"bot_owner_id": starlark.MakeInt64(e.tgOwner),
		"call":         starlark.NewBuiltin("call", e.callFunc(r.Context())),
		"json":         starlarkjson.Module,
		"raw_update":   starlark.String(rawUpdate),
		"time":         starlarktime.Module,
	}

	e.mu.Lock()
	botCode := bytes.Clone(e.bot)
	e.mu.Unlock()

	_, err = starlark.ExecFile(&starlark.Thread{}, "bot.star", botCode, predeclared)
	if err != nil {
		if evalErr, ok := err.(*starlark.EvalError); ok {
			web.Error(w, r, errors.New(evalErr.Backtrace()))
			return
		}
		web.Error(w, r, err)
		return
	}
}

func (e *engine) handleLogin(w http.ResponseWriter, r *http.Request) {
	// See https://core.telegram.org/widgets/login#receiving-authorization-data.
	data := r.URL.Query()
	hash := data.Get("hash")
	if hash == "" {
		web.Error(w, r, fmt.Errorf("no hash present in auth data, got: %v", data))
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
		web.Error(w, r, errors.New("hash is not valid"))
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

	// Check if ID of authenticated user matches the bot owner ID.
	for _, line := range strings.Split(data, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "id" {
			return strings.TrimSpace(parts[1]) == strconv.FormatInt(e.tgOwner, 10)
		}
	}
	return false
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

// selfPing continusly pings Starlet every 10 minutes in production to prevent
// it's Render app from sleeping.
func (e *engine) selfPing(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	log.Printf("selfPing: started")
	defer log.Printf("selfPing: stopped")

	for {
		select {
		case <-ticker.C:
			url := os.Getenv("RENDER_EXTERNAL_URL")
			if url == "" {
				log.Printf("selfPing: RENDER_EXTERNAL_URL is not set; are you really on Render?")
				return
			}
			health, err := httputil.MakeRequest[web.HealthResponse](ctx, httputil.RequestParams{
				Method:     http.MethodGet,
				URL:        url + "/health",
				HTTPClient: e.httpc,
			})
			if err != nil {
				log.Printf("selfPing: %v", err)
			}
			if !health.OK {
				log.Printf("selfPing: unhealthy: %+v", health)
			}
		case <-ctx.Done():
			return
		}
	}
}

type starlarkBuiltin func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error)

func (e *engine) callFunc(ctx context.Context) starlarkBuiltin {
	return func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(args) > 0 {
			return starlark.None, fmt.Errorf("call: unexpected positional arguments")
		}
		var (
			method   starlark.String
			argsDict *starlark.Dict
		)
		if err := starlark.UnpackArgs("call", args, kwargs, "method", &method, "args", &argsDict); err != nil {
			return starlark.None, err
		}

		encode := starlarkjson.Module.Members["encode"]
		val, err := starlark.Call(thread, encode, starlark.Tuple{argsDict}, []starlark.Tuple{})
		if err != nil {
			return starlark.None, err
		}
		str, ok := val.(starlark.String)
		if !ok {
			panic("call: unexpected return type of json.encode Starlark function")
		}

		if _, err := httputil.MakeRequest[any](ctx, httputil.RequestParams{
			Method:     http.MethodPost,
			URL:        "https://api.telegram.org/bot" + e.tgToken + "/" + string(method),
			Body:       json.RawMessage(str),
			HTTPClient: e.httpc,
		}); err != nil {
			return starlark.None, err
		}

		return starlark.None, nil
	}
}

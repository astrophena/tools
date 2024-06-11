// starlet implements a Telegram bot written in Starlark language.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
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
	"strings"
	"syscall"
	"time"

	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/httputil"
	"go.astrophena.name/tools/internal/version"
	"go.astrophena.name/tools/internal/web"

	starlarkjson "go.starlark.net/lib/json"
	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
)

//go:embed bot.star
var bot []byte

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

	e := &engine{
		tgToken:  *tgToken,
		tgSecret: *tgSecret,
		httpc: &http.Client{
			Timeout: 10 * time.Second,
		},
		mux: http.NewServeMux(),
	}
	e.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			web.NotFound(w, r)
			return
		}
		if !e.loggedIn(r) {
			http.Redirect(w, r, "https://t.me/astrophena_bot", http.StatusFound)
			return
		}
		w.Write([]byte("hello, world!"))
	})
	e.mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, version.Version().Short())
	})
	e.mux.HandleFunc("GET /login", e.handleLogin)
	e.mux.HandleFunc("POST /telegram", e.handleTelegramWebhook)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if isProd() {
		go e.selfPing(ctx)
	}

	web.ListenAndServe(ctx, &web.ListenAndServeConfig{
		Mux:        e.mux,
		Addr:       *addr,
		Debuggable: !isProd(),
	})
}

// https://docs.render.com/environment-variables
func isProd() bool { return os.Getenv("RENDER") == "true" }

type engine struct {
	tgToken  string
	tgSecret string

	httpc *http.Client
	mux   *http.ServeMux
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
		"raw_update": starlark.String(rawUpdate),
		"time":       starlarktime.Module,
		"json":       starlarkjson.Module,
		"call":       starlark.NewBuiltin("call", e.callFunc(r.Context())),
	}

	_, err = starlark.ExecFile(&starlark.Thread{}, "bot.star", bot, predeclared)
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

	setCookie(w, "auth_data", checkString)
	setCookie(w, "auth_data_hash", hash)

	http.Redirect(w, r, "/", http.StatusFound)
}

func (e *engine) loggedIn(r *http.Request) bool {
	if len(r.Cookies()) == 0 {
		return false
	}
	var data, hash string
	for _, cookie := range r.Cookies() {
		switch cookie.Name {
		case "auth_data":
			data = cookie.Value
		case "auth_data_hash":
			hash = cookie.Value
		}
	}
	return e.validateAuthData(data, hash)
}

func setCookie(w http.ResponseWriter, key, val string) {
	http.SetCookie(w, &http.Cookie{
		Name:     key,
		Value:    val,
		Expires:  time.Now().Add(time.Hour * 24 * 365), // approx one year
		HttpOnly: true,
	})
}

func (e *engine) validateAuthData(data, hash string) bool {
	return hmacSig(data, e.tgToken) == hash
}

func hmacSig(message, token string) string {
	h := hmac.New(sha256.New, toSHA256(token))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

func toSHA256(s string) []byte {
	h := sha256.New()
	h.Write([]byte(s))
	return h.Sum(nil)
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

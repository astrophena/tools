// starlet implements a Telegram bot written in Starlark language.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	e.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { web.NotFound(w, r) })
	e.mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, version.Version().Short())
	})
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

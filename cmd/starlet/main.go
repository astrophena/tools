// starlet implements a Telegram bot written in [Starlark] language.
//
// [Starlark]: https://github.com/bazelbuild/starlark
package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"go.astrophena.name/tools/internal/cli"
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
	e.mux.HandleFunc("POST /telegram", e.handleTelegramWebhook)

	web.ListenAndServe(&web.ListenAndServeConfig{
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
		"call":       starlark.NewBuiltin("call", e.call),
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

func (e *engine) call(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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

	req, err := http.NewRequest(http.MethodPost, "https://api.telegram.org/bot"+e.tgToken+"/"+string(method), strings.NewReader(string(str)))
	if err != nil {
		return starlark.None, err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := e.httpc.Do(req)
	if err != nil {
		return starlark.None, err
	}
	defer res.Body.Close()

	bs, err := io.ReadAll(res.Body)
	if err != nil {
		return starlark.None, err
	}
	if res.StatusCode != http.StatusOK {
		return starlark.None, fmt.Errorf("want 200, got %d: %s", res.StatusCode, bs)
	}

	return starlark.None, nil
}

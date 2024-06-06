// starlet implements a Telegram bot written in [Starlark] language.
//
// [Starlark]: https://github.com/bazelbuild/starlark
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"

	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/web"

	"go.starlark.net/lib/json"
	"go.starlark.net/lib/time"
	"go.starlark.net/starlark"
)

func main() {
	cli.SetDescription("starlet is a Telegram bot engine.")
	var (
		addr = flag.String("addr", "localhost:3000", "Listen on `host:port or Unix socket`.")
	)
	cli.HandleStartup()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handle)

	web.ListenAndServe(&web.ListenAndServeConfig{
		Mux:  mux,
		Addr: *addr,
	})
}

func handle(w http.ResponseWriter, r *http.Request) {
	rawUpdate, err := io.ReadAll(r.Body)
	if err != nil {
		errorf(w, err)
		return
	}
	predeclared := starlark.StringDict{
		"raw_update": starlark.String(rawUpdate),
		"time":       time.Module,
		"json":       json.Module,
	}
	_, err = starlark.ExecFile(&starlark.Thread{}, "bot.star", nil, predeclared)
	if err != nil {
		if evalErr, ok := err.(*starlark.EvalError); ok {
			errorf(w, evalErr.Backtrace())
			return
		}
		errorf(w, err)
		return
	}
}

func errorf(w http.ResponseWriter, arg any) {
	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprintln(w, arg)
}

// Starbuck is a HTTP server that runs on https://exp.astrophena.name.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"

	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/systemd"
	"go.astrophena.name/tools/internal/version"
	"go.astrophena.name/tools/internal/web"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	cli.Run(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	a := &cli.App{
		Name:        "starbuck",
		Description: helpDoc,
		Flags:       flag.NewFlagSet("starbuck", flag.ContinueOnError),
	}
	var (
		addr = a.Flags.String("addr", "localhost:3000", "Listen on `host:port`.")
	)
	if err := a.HandleStartup(args, stdout, stderr); err != nil {
		if errors.Is(err, cli.ErrExitVersion) {
			return nil
		}
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		web.RespondError(log.Printf, w, web.ErrNotFound)
	})
	mux.HandleFunc("/reqinfo", func(w http.ResponseWriter, r *http.Request) {
		b, err := httputil.DumpRequest(r, true)
		if err != nil {
			web.RespondError(log.Printf, w, err)
			return
		}
		w.Write(b)
	})
	mux.HandleFunc("/sha", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, version.Version().Commit)
	})

	systemd.Notify(log.Printf, systemd.Ready)
	go systemd.WatchdogLoop(ctx, log.Printf)

	return web.ListenAndServe(ctx, &web.ListenAndServeConfig{
		Addr:       *addr,
		Mux:        mux,
		Debuggable: false,
	})
}

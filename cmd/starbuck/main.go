// Starbuck is a HTTP server that runs on https://exp.astrophena.name.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/web"
)

func main() {
	var (
		addr = flag.String("addr", "localhost:3000", "Listen on `host:port`.")
	)
	cli.HandleStartup()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		web.RespondError(log.Printf, w, web.ErrNotFound)
	})

	if err := web.ListenAndServe(ctx, &web.ListenAndServeConfig{
		Addr:       *addr,
		Mux:        mux,
		Debuggable: false,
	}); err != nil {
		log.Fatal(err)
	}
}

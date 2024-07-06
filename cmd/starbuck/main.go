// Starbuck is a HTTP server that runs on https://exp.astrophena.name.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/cli/systemd"
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
	mux.HandleFunc("/reqinfo", func(w http.ResponseWriter, r *http.Request) {
		b, err := httputil.DumpRequest(r, true)
		if err != nil {
			web.RespondError(log.Printf, w, err)
			return
		}
		w.Write(b)
	})

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := web.ListenAndServe(ctx, &web.ListenAndServeConfig{
			Addr:       *addr,
			Mux:        mux,
			Debuggable: false,
		}); err != nil {
			log.Fatal(err)
		}
	}()

	systemd.Notify(log.Printf, systemd.Ready)
	go systemd.RunWatchdog(ctx, log.Printf)

	wg.Wait()
}

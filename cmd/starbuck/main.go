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
	"sync"
	"sync/atomic"

	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/logger"
	"go.astrophena.name/tools/internal/systemd"
	"go.astrophena.name/tools/internal/version"
	"go.astrophena.name/tools/internal/web"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	s := new(server)
	cli.Run(s.run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

type server struct {
	running atomic.Bool
	init    sync.Once
	logf    logger.Logf
	mux     *http.ServeMux
	stderr  io.Writer // os.Stderr if not set
}

var errAlreadyRunning = errors.New("already running")

func (s *server) run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	// Check if this server is already running.
	if s.running.Load() {
		return errAlreadyRunning
	}
	s.running.Store(true)
	defer s.running.Store(false)

	// Initialize internal state.
	s.stderr = stderr
	s.init.Do(s.doInit)

	// Define and parse flags.
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

	// Notify systemd that we are ready and start watchdog loop.
	systemd.Notify(s.logf, systemd.Ready)
	go systemd.WatchdogLoop(ctx, s.logf)

	// Start serving web requests.
	return web.ListenAndServe(ctx, &web.ListenAndServeConfig{
		Addr:       *addr,
		Logf:       s.logf,
		Mux:        s.mux,
		Debuggable: false,
	})
}

func (s *server) doInit() {
	if s.stderr == nil {
		s.stderr = os.Stderr
	}
	s.logf = log.New(s.stderr, "", 0).Printf

	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		web.RespondError(s.logf, w, web.ErrNotFound)
	})
	s.mux.HandleFunc("GET /reqinfo", func(w http.ResponseWriter, r *http.Request) {
		b, err := httputil.DumpRequest(r, true)
		if err != nil {
			web.RespondError(s.logf, w, err)
			return
		}
		w.Write(b)
	})
	s.mux.HandleFunc("GET /sha", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, version.Version().Commit)
	})
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

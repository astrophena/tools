package web

import (
	"bytes"
	"context"
	_ "embed"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.astrophena.name/tools/internal/logger"
)

//go:generate curl -o style.css https://astrophena.name/css/main.css

//go:embed style.css
var style []byte

// ListenAndServeConfig is used to configure the HTTP server started by
// ListenAndServe function.
type ListenAndServeConfig struct {
	// Addr is a network address to listen on ('host:port' or Unix socket absolute
	// path).
	Addr string
	// Mux is a http.ServeMux to serve.
	Mux *http.ServeMux
	// Logf specifies a logger to use. If nil, log.Printf is used.
	Logf logger.Logf
	// AfterShutdown specifies an optional callback function that is called when
	// the server was shut down.
	AfterShutdown func()
}

func (c *ListenAndServeConfig) fatalf(format string, args ...any) {
	c.Logf(format, args...)
	os.Exit(1)
}

// ListenAndServe starts the HTTP server based on the provided
// ListenAndServeConfig.
func ListenAndServe(c *ListenAndServeConfig) {
	if c.Logf == nil {
		c.Logf = log.Printf
	}
	if c.Addr == "" {
		c.fatalf("web.ListenAndServe(): Addr is empty")
	}
	if c.Mux == nil {
		c.fatalf("web.ListenAndServe(): Mux is nil")
	}

	network := "tcp"
	if strings.HasPrefix(c.Addr, "/") {
		network = "unix"
		os.Remove(c.Addr)
	}

	l, err := net.Listen(network, c.Addr)
	if err != nil {
		c.fatalf("Failed to listen: %v", err)
	}
	defer l.Close()
	c.Logf("Listening on %s://%s...", l.Addr().Network(), l.Addr().String())

	if network == "unix" {
		if err := os.Chmod(c.Addr, 0o666); err != nil {
			c.fatalf("Failed to change socket permissions: %v", err)
		}
	}

	c.Mux.HandleFunc("/style.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "style.css", time.Time{}, bytes.NewReader(style))
	})
	Health(c.Mux)
	Debugger(c.Mux)
	s := &http.Server{Handler: c.Mux}
	go func() {
		if err := s.Serve(l); err != nil {
			if err != http.ErrServerClosed {
				c.fatalf("HTTP server crashed: %v", err)
			}
		}
	}()

	exit := make(chan os.Signal, 1)
	signal.Notify(exit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	sig := <-exit
	c.Logf("Received %s, gracefully shutting down...", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.Shutdown(ctx)
	if c.AfterShutdown != nil {
		c.AfterShutdown()
	}
}

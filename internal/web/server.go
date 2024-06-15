package web

import (
	"bytes"
	"context"
	_ "embed"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
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
	// Debuggable specifies whether to register debug handlers at /debug/.
	Debuggable bool
	// DebugAuth specifies an optional function that's invoked on every request to
	// debug handlers at /debug/ to allow or deny access to them. If not provided,
	// all access is allowed.
	DebugAuth func(r *http.Request) bool
}

func (c *ListenAndServeConfig) fatalf(format string, args ...any) {
	c.Logf(format, args...)
	os.Exit(1)
}

// ListenAndServe starts the HTTP server based on the provided
// ListenAndServeConfig.
func ListenAndServe(ctx context.Context, c *ListenAndServeConfig) {
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
	if c.Debuggable {
		Debugger(c.Logf, c.Mux)
	}

	protectDebug := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/debug/") || c.DebugAuth == nil {
				next.ServeHTTP(w, r)
				return
			}
			// If access denied, pretend that debug endpoints don't exist.
			if !c.DebugAuth(r) {
				NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	s := &http.Server{Handler: protectDebug(c.Mux)}
	go func() {
		if err := s.Serve(l); err != nil {
			if err != http.ErrServerClosed {
				c.fatalf("HTTP server crashed: %v", err)
			}
		}
	}()

	<-ctx.Done()
	c.Logf("Gracefully shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.Shutdown(shutdownCtx)
	if c.AfterShutdown != nil {
		c.AfterShutdown()
	}
}

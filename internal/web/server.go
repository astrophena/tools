package web

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"go.astrophena.name/tools/internal/logger"
)

//go:generate curl --fail-with-body -s -o static/style.css https://astrophena.name/css/main.css

//go:embed static/style.css
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

// used in tests
var serveReadyHook func()

// ListenAndServe starts the HTTP server based on the provided
// ListenAndServeConfig.
func ListenAndServe(ctx context.Context, c *ListenAndServeConfig) error {
	if c.Logf == nil {
		c.Logf = log.Printf
	}
	if c.Addr == "" {
		return errors.New("Addr is empty")
	}
	if c.Mux == nil {
		return errors.New("Mux is nil")
	}

	network := "tcp"
	if strings.HasPrefix(c.Addr, "/") {
		network = "unix"
		os.Remove(c.Addr)
	}

	l, err := net.Listen(network, c.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	defer l.Close()
	c.Logf("Listening on %s://%s...", l.Addr().Network(), l.Addr().String())

	if network == "unix" {
		if err := os.Chmod(c.Addr, 0o666); err != nil {
			return fmt.Errorf("failed to change socket permissions: %v", err)
		}
	}

	protectDebug := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/debug/") || c.DebugAuth == nil {
				next.ServeHTTP(w, r)
				return
			}
			// If access denied, pretend that debug endpoints don't exist.
			if !c.DebugAuth(r) {
				RespondError(c.Logf, w, ErrNotFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	s := &http.Server{Handler: protectDebug(c.Mux)}
	c.initInternalRoutes(s)

	errCh := make(chan error, 1)

	go func() {
		if err := s.Serve(l); err != nil {
			if err != http.ErrServerClosed {
				errCh <- err
			}
		}
	}()

	if serveReadyHook != nil {
		serveReadyHook()
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		c.Logf("Gracefully shutting down...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := s.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if c.AfterShutdown != nil {
			c.AfterShutdown()
		}
	}

	return nil
}

func (c ListenAndServeConfig) initInternalRoutes(s *http.Server) {
	c.Mux.HandleFunc("/style.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "style.css", time.Time{}, bytes.NewReader(style))
	})
	Health(c.Mux)
	if c.Debuggable {
		dbg := Debugger(c.Logf, c.Mux)
		dbg.Handle("conns", "Connections", Conns(c.Logf, s))
	}
}

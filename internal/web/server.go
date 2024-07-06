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
	"path/filepath"
	"strings"
	"time"

	"go.astrophena.name/tools/internal/logger"
	"go.astrophena.name/tools/internal/version"

	"golang.org/x/crypto/acme/autocert"
)

//go:generate curl --fail-with-body -s -o static/style.css https://astrophena.name/css/main.css

//go:embed static/style.css
var style []byte

// ListenAndServeConfig is used to configure the HTTP server started by
// ListenAndServe function.
type ListenAndServeConfig struct {
	// Addr is a network address to listen on (in the form of "host:port").  If
	// Addr is not localhost and doesn't contain a port (in example, "example.com"
	// or "exp.astrophena.name"), the server accepts HTTPS connections and
	// automatically obtains a certificate from Let's Encrypt.
	Addr string
	// Mux is a http.ServeMux to serve.
	Mux *http.ServeMux
	// Logf specifies a logger to use. If nil, log.Printf is used.
	Logf logger.Logf
	// Debuggable specifies whether to register debug handlers at /debug/.
	Debuggable bool
	// DebugAuth specifies an optional function that's invoked on every request to
	// debug handlers at /debug/ to allow or deny access to them. If not provided,
	// all access is allowed.
	DebugAuth func(r *http.Request) bool
}

// used in tests
var serveReadyHook func()

var (
	errNoAddr = errors.New("c.Addr is empty")
	errNilMux = errors.New("c.Mux is nil")
)

// ListenAndServe starts the HTTP server based on the provided
// ListenAndServeConfig.
func ListenAndServe(ctx context.Context, c *ListenAndServeConfig) error {
	if c.Logf == nil {
		c.Logf = log.Printf
	}
	if c.Addr == "" {
		return errNoAddr
	}
	if c.Mux == nil {
		return errNilMux
	}

	l, err := obtainListener(c)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	defer l.Close()
	c.Logf("Listening on %s://%s...", l.Addr().Network(), l.Addr().String())

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
	initInternalRoutes(c, s)

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
	}

	return nil
}

func initInternalRoutes(c *ListenAndServeConfig, s *http.Server) {
	c.Mux.HandleFunc("/style.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "style.css", time.Time{}, bytes.NewReader(style))
	})
	Health(c.Mux)
	if c.Debuggable {
		dbg := Debugger(c.Logf, c.Mux)
		dbg.Handle("conns", "Connections", Conns(c.Logf, s))
	}
}

func obtainListener(c *ListenAndServeConfig) (net.Listener, error) {
	host, _, hasPort := strings.Cut(c.Addr, ":")

	// Accept HTTPS connections only and obtain Let's Encrypt certificate.
	if host != "localhost" && !hasPort {
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(cacheDir()),
			HostPolicy: autocert.HostWhitelist(c.Addr),
		}
		return m.Listener(), nil
	}

	return net.Listen("tcp", c.Addr)
}

func cacheDir() string {
	// Three variants where we can keep certificate cache:
	//
	//  a. in cache directory (i.e. ~/.cache on Linux)
	//  b. in systemd unit state directory (i.e. /var/lib/private/...)
	//  c. in OS temporary directory if everything fails
	//
	// Case b overrides a and c used as a last resort.
	dir, err := os.UserCacheDir()
	if stateDir := os.Getenv("STATE_DIRECTORY"); stateDir != "" {
		return filepath.Join(stateDir, "certs")
	} else if err != nil {
		return filepath.Join(os.TempDir(), version.Version().Name, "certs")
	}
	return filepath.Join(dir, version.Version().Name, "certs")
}

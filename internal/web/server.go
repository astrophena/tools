// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package web

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/syncx"
	"go.astrophena.name/base/version"

	"github.com/benbjohnson/hashfs"
	"golang.org/x/crypto/acme/autocert"
)

//go:generate curl --fail-with-body -s -o static/css/main.css https://astrophena.name/css/main.css

// Server is used to configure the HTTP server started by
// [Server.ListenAndServe].
//
// All fields of Server can't be modified after [Server.ListenAndServe]
// or [Server.ServeHTTP] is called for a first time.
type Server struct {
	// Mux is a http.ServeMux to serve.
	Mux *http.ServeMux
	// Debuggable specifies whether to register debug handlers at /debug/.
	Debuggable bool
	// Middleware specifies an optional slice of HTTP middleware that's applied to
	// each request.
	Middleware []Middleware

	// Addr is a network address to listen on (in the form of "host:port").
	//
	// If Addr is not localhost and doesn't contain a port (in example,
	// "example.com" or "exp.astrophena.name"), the server accepts HTTPS
	// connections on :443, redirects HTTP connections to HTTPS on :80 and
	// automatically obtains a certificate from Let's Encrypt.
	Addr string
	// Ready specifies an optional function to be called when the server is ready
	// to serve requests.
	Ready func()

	handler syncx.Lazy[http.Handler]
}

// ServeHTTP implements the [http.Handler] interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.Get(s.initHandler).ServeHTTP(w, r)
}

// Stolen from https://github.com/tailscale/tailscale/blob/4ad3f01225745294474f1ae0de33e5a86824a744/safeweb/http.go.

// The Content-Security-Policy header.
var cspHeader = strings.Join([]string{
	`default-src 'self'`,      // origin is the only valid source for all content types
	`script-src 'self'`,       // disallow inline javascript
	`frame-ancestors 'none'`,  // disallow framing of the page
	`form-action 'self'`,      // disallow form submissions to other origins
	`base-uri 'self'`,         // disallow base URIs from other origins
	`block-all-mixed-content`, // disallow mixed content when serving over HTTPS
	`object-src 'self'`,       // disallow embedding of resources from other origins
}, "; ")

// The Strict-Transport-Security header. This header tells the browser
// to exclusively use HTTPS for all requests to the origin for the next year.
const hstsHeader = "max-age=31536000"

var (
	errNoAddr = errors.New("server.Addr is empty")
	errListen = errors.New("failed to listen")
)

type Middleware func(http.Handler) http.Handler

var defaultMiddleware = []Middleware{
	setHeaders,
}

func setHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", cspHeader)
		if isHTTPS(r) {
			w.Header().Set("Strict-Transport-Security", hstsHeader)
		}
		next.ServeHTTP(w, r)
	})
}

func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	return false
}

func (s *Server) initHandler() http.Handler {
	if s.Mux == nil {
		panic("Server.Mux is nil")
	}

	// Initialize internal routes.
	s.Mux.Handle("GET /static/", hashfs.FileServer(StaticFS))
	s.Mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) { RespondJSON(w, version.Version()) })
	Health(s.Mux)
	if s.Debuggable {
		Debugger(s.Mux)
	}

	// Apply middleware.
	var handler http.Handler = s.Mux
	mws := append(defaultMiddleware, s.Middleware...)
	for _, middleware := range slices.Backward(mws) {
		handler = middleware(handler)
	}

	return handler
}

// ListenAndServe starts the HTTP server that can be stopped by canceling ctx.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if s.Addr == "" {
		return errNoAddr
	}

	l, isTLS, err := obtainListener(s)
	if err != nil {
		return fmt.Errorf("%w: %v", errListen, err)
	}
	defer l.Close()

	scheme, host := "http", l.Addr().String()
	if isTLS {
		scheme, host = "https", s.Addr
	}

	logf := cli.GetEnv(ctx).Logf

	logf("Listening on %s://%s...", scheme, host)

	// Redirect HTTP requests to HTTPS.
	if isTLS {
		go httpRedirect(ctx)
	}

	httpSrv := &http.Server{
		ErrorLog: log.New(logger.Logf(logf), "", 0),
		Handler:  s,
		BaseContext: func(_ net.Listener) context.Context {
			return cli.WithEnv(context.Background(), cli.GetEnv(ctx))
		},
	}

	errCh := make(chan error, 1)

	go func() {
		if err := httpSrv.Serve(l); err != nil {
			if err != http.ErrServerClosed {
				errCh <- err
			}
		}
	}()

	if s.Ready != nil {
		s.Ready()
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logf("Gracefully shutting down...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			return err
		}
	}

	return nil
}

//go:embed static
var staticFS embed.FS

// StaticFS is an [embed.FS] that contains static resources served on /static/ path
// prefix of [Server] HTTP handlers.
var StaticFS = hashfs.NewFS(staticFS)

func obtainListener(c *Server) (l net.Listener, isTLS bool, err error) {
	host, _, hasPort := strings.Cut(c.Addr, ":")

	// Accept HTTPS connections only and obtain Let's Encrypt certificate.
	if host != "localhost" && !hasPort {
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(cacheDir()),
			HostPolicy: autocert.HostWhitelist(c.Addr),
		}
		return m.Listener(), true, nil
	}

	l, err = net.Listen("tcp", c.Addr)
	return l, false, err
}

func cacheDir() string {
	// Three variants where we can keep certificate cache:
	//
	//  a. in systemd unit state directory (i.e. /var/lib/private/...)
	//  b. in cache directory (i.e. ~/.cache on Linux)
	//  c. in OS temporary directory if everything fails
	//
	dir, err := os.UserCacheDir()
	if stateDir := os.Getenv("STATE_DIRECTORY"); stateDir != "" {
		return filepath.Join(stateDir, "certs")
	} else if err != nil {
		return filepath.Join(os.TempDir(), version.Version().Name, "certs")
	}
	return filepath.Join(dir, version.Version().Name, "certs")
}

// httpRedirect redirects HTTP requests to HTTPS and runs in a separate
// goroutine.
func httpRedirect(ctx context.Context) {
	logf := cli.GetEnv(ctx).Logf
	s := &http.Server{
		Addr: ":80",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := "https://" + r.Host + r.URL.Path
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		}),
	}
	go func() {
		<-ctx.Done()
		s.Shutdown(ctx)
	}()
	if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logf("web.Server: HTTP to HTTPS redirect goroutine crashed: %v", err)
	}
}

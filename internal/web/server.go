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
	"strings"
	"time"

	"go.astrophena.name/base/logger"
	"go.astrophena.name/tools/internal/version"

	"github.com/benbjohnson/hashfs"
	"golang.org/x/crypto/acme/autocert"
)

//go:generate curl --fail-with-body -s -o static/css/main.css https://astrophena.name/css/main.css

// ListenAndServeConfig is used to configure the HTTP server started by
// [ListenAndServe].
//
// All fields of ListenAndServeConfig can't be modified after [ListenAndServe]
// is called.
type ListenAndServeConfig struct {
	// Addr is a network address to listen on (in the form of "host:port").
	//
	// If Addr is not localhost and doesn't contain a port (in example,
	// "example.com" or "exp.astrophena.name"), the server accepts HTTPS
	// connections on :443, redirects HTTP connections to HTTPS on :80 and
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
	// Ready specifies an optional function to be called when the server is ready
	// to serve requests.
	Ready func()
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
	errNoAddr = errors.New("c.Addr is empty")
	errNilMux = errors.New("c.Mux is nil")
)

// ListenAndServe starts the HTTP server based on the provided
// [ListenAndServeConfig].
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

	l, isTLS, err := obtainListener(c)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	defer l.Close()

	scheme, host := "http", l.Addr().String()
	if isTLS {
		scheme, host = "https", c.Addr
	}

	c.Logf("Listening on %s://%s...", scheme, host)

	// Redirect HTTP requests to HTTPS.
	if isTLS {
		go httpRedirect(ctx, c)
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

	setHeaders := func(next http.Handler) http.Handler {
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

	s := &http.Server{
		ErrorLog: log.New(c.Logf, "", 0),
		Handler:  setHeaders(protectDebug(c.Mux)),
	}
	initInternalRoutes(c)

	errCh := make(chan error, 1)

	go func() {
		if err := s.Serve(l); err != nil {
			if err != http.ErrServerClosed {
				errCh <- err
			}
		}
	}()

	if c.Ready != nil {
		c.Ready()
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

func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	return false
}

//go:embed static
var staticFS embed.FS

// StaticFS is an [embed.FS] that contains static resources served on /static/ path
// prefix of [ListenAndServe] servers.
var StaticFS = hashfs.NewFS(staticFS)

func initInternalRoutes(c *ListenAndServeConfig) {
	c.Mux.Handle("GET /static/", hashfs.FileServer(StaticFS))
	c.Mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) { RespondJSON(w, version.Version()) })
	Health(c.Mux)
	if c.Debuggable {
		Debugger(c.Logf, c.Mux)
	}
}

func obtainListener(c *ListenAndServeConfig) (l net.Listener, isTLS bool, err error) {
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

// httpRedirect redirects HTTP requests to HTTPS and runs in a separate
// goroutine.
func httpRedirect(ctx context.Context, c *ListenAndServeConfig) {
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
		c.Logf("httpRedirect crashed: %v", err)
	}
}

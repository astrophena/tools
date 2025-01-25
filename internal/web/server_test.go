// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/logger"
	"go.astrophena.name/base/testutil"
)

func TestServerConfig(t *testing.T) {
	cases := map[string]struct {
		s       *Server
		wantErr error
	}{
		"no Addr": {
			s: &Server{
				Addr: "",
				Mux:  http.NewServeMux(),
			},
			wantErr: errNoAddr,
		},
		"invalid port": {
			s: &Server{
				Addr: ":100000",
				Mux:  http.NewServeMux(),
			},
			wantErr: errListen,
		},
	}
	for _, tc := range cases {
		err := tc.s.ListenAndServe(context.Background())

		// Don't use && because we want to trap all cases where err is nil.
		if err == nil {
			if tc.wantErr != nil {
				t.Fatalf("must fail with error: %v", tc.wantErr)
			}
		}

		if err != nil && !errors.Is(err, tc.wantErr) {
			t.Fatalf("got error: %v", err)
		}
	}
}

func TestServerHTTPS(t *testing.T) {
	s := &Server{
		Mux: http.NewServeMux(),
	}

	ts := httptest.NewTLSServer(s)
	t.Cleanup(func() {
		ts.Close()
	})

	req, err := ts.Client().Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer req.Body.Close()

	if hstsHeader := req.Header.Get("Strict-Transport-Security"); hstsHeader == "" {
		t.Error("Strict-Transport-Security is not set for HTTPS requests")
	}
}

func TestServerListenAndServe(t *testing.T) {
	// Find a free port for us.
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to find a free port: %v", err)
	}
	addr := fmt.Sprintf("localhost:%d", port)

	var wg sync.WaitGroup

	ready := make(chan struct{})
	readyFunc := func() {
		ready <- struct{}{}
	}
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())

	env := &cli.Env{
		Stderr: logger.Logf(t.Logf),
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		s := &Server{
			Addr:       addr,
			Mux:        http.NewServeMux(),
			Debuggable: true,
			Ready:      readyFunc,
		}
		if err := s.ListenAndServe(cli.WithEnv(ctx, env)); err != nil {
			errCh <- err
		}
	}()

	// Wait until the server is ready.
	select {
	case err := <-errCh:
		t.Fatalf("Test server crashed during startup or runtime: %v", err)
	case <-ready:
	}

	// Make some HTTP requests.
	urls := []struct {
		url        string
		wantStatus int
	}{
		{url: "/static/css/main.css", wantStatus: http.StatusOK},
		{url: "/static/" + StaticFS.HashName("css/main.css"), wantStatus: http.StatusOK},
		{url: "/health", wantStatus: http.StatusOK},
		{url: "/version", wantStatus: http.StatusOK},
	}

	for _, u := range urls {
		req, err := http.Get("http://" + addr + u.url)
		if err != nil {
			t.Fatal(err)
		}
		if req.StatusCode != u.wantStatus {
			t.Fatalf("GET %s: want status code %d, got %d", u.url, u.wantStatus, req.StatusCode)
		}
		testutil.AssertEqual(t, req.Header.Get("X-Content-Type-Options"), "nosniff")
		testutil.AssertEqual(t, req.Header.Get("Referer-Policy"), "same-origin")
		testutil.AssertEqual(t, req.Header.Get("Content-Security-Policy"), cspHeader)
	}

	// Try to gracefully shutdown the server.
	cancel()
	// Wait until the server shuts down.
	wg.Wait()
	// See if the server failed to shutdown.
	select {
	case err := <-errCh:
		t.Fatalf("Test server crashed during shutdown: %v", err)
	default:
	}
}

// getFreePort asks the kernel for a free open port that is ready to use.
// Copied from
// https://github.com/phayes/freeport/blob/74d24b5ae9f58fbe4057614465b11352f71cdbea/freeport.go.
func getFreePort() (port int, err error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

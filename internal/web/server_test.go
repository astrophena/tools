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
	"sync"
	"testing"

	"go.astrophena.name/base/testutil"
)

func TestListenAndServeConfig(t *testing.T) {
	cases := map[string]struct {
		c       *ListenAndServeConfig
		wantErr error
	}{
		"no Addr": {
			c: &ListenAndServeConfig{
				Addr: "",
				Mux:  http.NewServeMux(),
			},
			wantErr: errNoAddr,
		},
		"nil Mux": {
			c: &ListenAndServeConfig{
				Addr: ":3000",
				Mux:  nil,
			},
			wantErr: errNilMux,
		},
	}
	for _, tc := range cases {
		err := ListenAndServe(context.Background(), tc.c)

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

func TestListenAndServe(t *testing.T) {
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

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := ListenAndServe(ctx, &ListenAndServeConfig{
			Addr:       addr,
			Mux:        http.NewServeMux(),
			Logf:       t.Logf,
			Debuggable: true,
			Ready:      readyFunc,
		}); err != nil {
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

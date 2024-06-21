package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
)

func TestListenAndServe(t *testing.T) {
	// Find a free port for us.
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to find a free port: %v", err)
	}
	addr := fmt.Sprintf("localhost:%d", port)

	var wg sync.WaitGroup

	ready := make(chan struct{})
	serveReadyHook = func() {
		ready <- struct{}{}
	}
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := ListenAndServe(ctx, &ListenAndServeConfig{
			Addr: addr,
			Mux:  http.NewServeMux(),
			Logf: t.Logf,
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
		{url: "/style.css", wantStatus: http.StatusOK},
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

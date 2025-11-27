// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package idle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestTracker(t *testing.T) {
	os.Setenv("EXIT_IDLE_TIME", "50ms")
	t.Cleanup(func() { os.Unsetenv("EXIT_IDLE_TIME") })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tracker := newTracker(cancel, func() bool { return true })
	if tracker == nil {
		t.Fatal("newTracker() = nil, want non-nil")
	}
	tracker.lastActivity.Store(time.Now().Add(-1 * time.Hour).Unix())

	go tracker.runActivityMonitor(ctx, 10*time.Millisecond)

	select {
	case <-ctx.Done():
		// Test passed.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context was not canceled")
	}
}

func TestTracker_Handler(t *testing.T) {
	tracker := &Tracker{}
	tracker.lastActivity.Store(time.Now().Add(-1 * time.Hour).Unix())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler := tracker.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	handler.ServeHTTP(rr, req)

	if time.Since(time.Unix(tracker.lastActivity.Load(), 0)) > 1*time.Second {
		t.Error("lastActivity was not updated")
	}
}

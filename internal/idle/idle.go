// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package idle provides a helper for exiting services after a period of
// inactivity.
package idle

import (
	"context"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

// Tracker is an idle tracker.
type Tracker struct {
	lastActivity atomic.Int64
	exitIdleTime time.Duration
	cancel       context.CancelFunc
}

// NewTracker returns a new idle tracker. It returns nil if the functionality
// is disabled.
//
// It is enabled only when the EXIT_IDLE_TIME environment variable is set to a
// non-zero duration and the service is socket-activated.
func NewTracker(cancel context.CancelFunc) *Tracker {
	return newTracker(cancel, isSocketActivated)
}

func newTracker(cancel context.CancelFunc, isSocketActivated func() bool) *Tracker {
	if !isSocketActivated() {
		return nil
	}
	exitIdleTime, err := time.ParseDuration(os.Getenv("EXIT_IDLE_TIME"))
	if err != nil || exitIdleTime == 0 {
		return nil
	}
	t := &Tracker{
		exitIdleTime: exitIdleTime,
		cancel:       cancel,
	}
	t.lastActivity.Store(time.Now().Unix())
	return t
}

func isSocketActivated() bool {
	if os.Getenv("FORCE_SOCKET_ACTIVATED") == "1" {
		return true
	}
	// See https://man.archlinux.org/man/sd_listen_fds.3.en#ENVIRONMENT
	return os.Getenv("LISTEN_PID") != ""
}

// Handler is a [web.Middleware] that updates the last activity time.
func (t *Tracker) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.lastActivity.Store(time.Now().Unix())
		next.ServeHTTP(w, r)
	})
}

// Run runs the activity monitor.
func (t *Tracker) Run(ctx context.Context) {
	go t.runActivityMonitor(ctx, 30*time.Second)
}

func (t *Tracker) runActivityMonitor(ctx context.Context, tickDuration time.Duration) {
	ticker := time.NewTicker(tickDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if time.Since(time.Unix(t.lastActivity.Load(), 0)) > t.exitIdleTime {
				t.cancel()
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

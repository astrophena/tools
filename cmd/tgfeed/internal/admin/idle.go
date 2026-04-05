// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package idle provides a helper for exiting services after a period of
// inactivity.
package admin

import (
	"context"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

// tracker is an idle tracker.
type tracker struct {
	lastActivity atomic.Int64
	exitIdleTime time.Duration
	cancel       context.CancelFunc
}

func newTracker(cancel context.CancelFunc, isSocketActivated func() bool) *tracker {
	if !isSocketActivated() {
		return nil
	}
	exitIdleTime, err := time.ParseDuration(os.Getenv("EXIT_IDLE_TIME"))
	if err != nil || exitIdleTime == 0 {
		return nil
	}
	t := &tracker{
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

// handler is a [web.Middleware] that updates the last activity time.
func (t *tracker) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.lastActivity.Store(time.Now().Unix())
		next.ServeHTTP(w, r)
	})
}

func (t *tracker) run(ctx context.Context) { go t.runActivityMonitor(ctx, 30*time.Second) }

func (t *tracker) runActivityMonitor(ctx context.Context, tickDuration time.Duration) {
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

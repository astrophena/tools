// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package ctxsleep provides a small context-aware sleep helper for retry
// loops.
package ctxsleep

import (
	"context"
	"time"
)

// Sleep waits for duration and reports whether the timer completed.
//
// It returns false when ctx is canceled before duration elapses.
func Sleep(ctx context.Context, duration time.Duration) bool {
	t := time.NewTimer(duration)
	defer t.Stop()

	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

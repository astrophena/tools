// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package ctxsleep waits for a timer or context cancellation.
package ctxsleep

import (
	"context"
	"time"
)

// Sleep waits for d and reports whether the timer elapsed.
func Sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

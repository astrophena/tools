// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package ctxsleep contains a context-aware sleep function.
package ctxsleep

import (
	"context"
	"time"
)

// Sleep waits for the duration unless the context is canceled first.
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

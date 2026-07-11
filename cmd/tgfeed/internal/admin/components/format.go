// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package components

import (
	"fmt"
	"math"
	"time"

	"go.astrophena.name/base/humanfmt"
)

func formatDateTime(t time.Time) string {
	if t.IsZero() {
		return "n/a"
	}
	return humanfmt.DateTime(t.UTC(), "%d.%m.%Y, %T %Z")
}

func formatRelativeTime(t, now time.Time) string {
	if t.IsZero() {
		return "n/a"
	}
	// The render timestamp is supplied by the page model to keep server output
	// deterministic within one response and straightforward to test.
	return humanfmt.RelativeTime(t, now)
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		return "n/a"
	}
	if d < time.Second {
		return fmt.Sprintf("%.0f ms", float64(d)/float64(time.Millisecond))
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1f s", d.Seconds())
	}
	return fmt.Sprintf("%dm %.0fs", int(d.Minutes()), math.Mod(d.Seconds(), 60))
}

func formatPercent(v float64, valid bool) string {
	if !valid {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", v)
}

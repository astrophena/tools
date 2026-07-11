// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package components

import (
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
)

func TestHumanFormatting(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		got  string
		want string
	}{
		"date and time": {
			got:  formatDateTime(now),
			want: "11.07.2026, 12:00:00 UTC",
		},
		"relative time": {
			got:  formatRelativeTime(now.Add(-3*time.Hour), now),
			want: "3 hours ago",
		},
		"zero date": {
			got:  formatDateTime(time.Time{}),
			want: "n/a",
		},
		"zero relative": {
			got:  formatRelativeTime(time.Time{}, now),
			want: "n/a",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testutil.AssertEqual(t, tc.got, tc.want)
		})
	}
}

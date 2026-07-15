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
		"relative hours": {
			got:  formatRelativeTime(now.Add(-3*time.Hour), now),
			want: "3h ago",
		},
		"relative seconds": {
			got:  formatRelativeTime(now.Add(-42*time.Second), now),
			want: "42s ago",
		},
		"relative minutes": {
			got:  formatRelativeTime(now.Add(-17*time.Minute), now),
			want: "17m ago",
		},
		"relative days": {
			got:  formatRelativeTime(now.Add(-49*time.Hour), now),
			want: "2d ago",
		},
		"future time": {
			got:  formatRelativeTime(now.Add(time.Second), now),
			want: "just now",
		},
		"sub-second milliseconds": {
			got:  formatMilliseconds(842),
			want: "842 ms",
		},
		"milliseconds as seconds": {
			got:  formatMilliseconds(20_838),
			want: "20.8 s",
		},
		"milliseconds as minutes": {
			got:  formatMilliseconds(75_000),
			want: "1m 15s",
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

func TestMemoryDelta(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		latest      uint64
		previous    uint64
		hasPrevious bool
		wantChange  string
		wantTone    string
	}{
		"no previous run": {
			latest:     1024,
			wantChange: "vs prev run: n/a",
			wantTone:   "neutral",
		},
		"unchanged": {
			latest:      1024,
			previous:    1024,
			hasPrevious: true,
			wantChange:  "no change vs prev run",
			wantTone:    "neutral",
		},
		"sub kibibyte change": {
			latest:      2047,
			previous:    1024,
			hasPrevious: true,
			wantChange:  "no change vs prev run",
			wantTone:    "neutral",
		},
		"increase": {
			latest:      7 * 1024 * 1024,
			previous:    1024 * 1024,
			hasPrevious: true,
			wantChange:  "+6 MiB vs prev run",
			wantTone:    "bad",
		},
		"decrease": {
			latest:      1536,
			previous:    4096,
			hasPrevious: true,
			wantChange:  "-2.5 KiB vs prev run",
			wantTone:    "good",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			change, tone := memoryDelta(tc.latest, tc.previous, tc.hasPrevious)
			testutil.AssertEqual(t, change, tc.wantChange)
			testutil.AssertEqual(t, tone, tc.wantTone)
		})
	}
}

func TestDurationDelta(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		latest      time.Duration
		previous    time.Duration
		hasPrevious bool
		wantChange  string
		wantTone    string
	}{
		"no previous run": {
			latest:     time.Second,
			wantChange: "vs prev run: n/a",
			wantTone:   "neutral",
		},
		"unchanged": {
			latest:      time.Second,
			previous:    time.Second,
			hasPrevious: true,
			wantChange:  "no change vs prev run",
			wantTone:    "neutral",
		},
		"millisecond increase": {
			latest:      1500 * time.Millisecond,
			previous:    time.Second,
			hasPrevious: true,
			wantChange:  "+500 ms vs prev run",
			wantTone:    "bad",
		},
		"second increase": {
			latest:      3500 * time.Millisecond,
			previous:    time.Second,
			hasPrevious: true,
			wantChange:  "+2.5 s vs prev run",
			wantTone:    "bad",
		},
		"minute decrease": {
			latest:      time.Minute,
			previous:    6 * time.Minute,
			hasPrevious: true,
			wantChange:  "-5m 0s vs prev run",
			wantTone:    "good",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			change, tone := durationDelta(tc.latest, tc.previous, tc.hasPrevious)
			testutil.AssertEqual(t, change, tc.wantChange)
			testutil.AssertEqual(t, tone, tc.wantTone)
		})
	}
}

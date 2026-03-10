// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package retry

import (
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
)

func TestUnknown(t *testing.T) {
	backoff, retryable := Retryable("invalid.localhost", []byte(`{}`))
	testutil.AssertEqual(t, backoff, time.Duration(0))
	testutil.AssertEqual(t, retryable, false)
}

func TestTelegramRSSBridge(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body  string
		want  time.Duration
		found bool
	}{
		"flood wait": {
			body:  `{"errors":["FLOOD_WAIT_42"]}`,
			want:  42 * time.Second,
			found: true,
		},
		"unlock access": {
			body:  `{"errors":["Time to unlock access: 01:02:03"]}`,
			want:  1*time.Hour + 2*time.Minute + 3*time.Second,
			found: true,
		},
		"mixed errors picks first valid": {
			body:  `{"errors":[123,"oops","FLOOD_WAIT_5"]}`,
			want:  5 * time.Second,
			found: true,
		},
		"unknown format": {
			body:  `{"errors":["something else"]}`,
			want:  0,
			found: false,
		},
		"invalid json": {
			body:  `{`,
			want:  0,
			found: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, found := Retryable("tg.i-c-a.su", []byte(tc.body))
			testutil.AssertEqual(t, found, tc.found)
			testutil.AssertEqual(t, got, tc.want)
		})
	}
}

func TestRetryAfter(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		header string
		want   time.Duration
		found  bool
	}{
		"empty": {
			header: "",
			want:   0,
			found:  false,
		},
		"delay-seconds": {
			header: "120",
			want:   120 * time.Second,
			found:  true,
		},
		"invalid delay-seconds": {
			header: "-5",
			want:   0,
			found:  false,
		},
		"http-date future": {
			header: time.Now().Add(5 * time.Minute).Format(time.RFC1123),
			want:   5 * time.Minute,
			found:  true, // exact duration might be slightly less, handled below
		},
		"http-date past": {
			header: time.Now().Add(-5 * time.Minute).Format(time.RFC1123),
			want:   0,
			found:  true,
		},
		"invalid format": {
			header: "not a date or number",
			want:   0,
			found:  false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, found := RetryAfter(tc.header)
			testutil.AssertEqual(t, found, tc.found)
			// For future dates, allow a small time drift delta.
			if tc.found && tc.want > 0 {
				diff := got - tc.want
				if diff < 0 {
					diff = -diff
				}
				if diff > time.Second {
					t.Errorf("RetryAfter(%q) duration = %v; want ~%v", tc.header, got, tc.want)
				}
			} else {
				testutil.AssertEqual(t, got, tc.want)
			}
		})
	}
}

// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package ctxsleep

import (
	"context"
	"testing"
	"time"
)

func TestSleep(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		context  func() context.Context
		duration time.Duration
		want     bool
	}{
		"elapsed": {
			context: func() context.Context {
				return t.Context()
			},
			duration: 1 * time.Millisecond,
			want:     true,
		},
		"canceled": {
			context: func() context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
			duration: time.Second,
			want:     false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := Sleep(tc.context(), tc.duration)
			if got != tc.want {
				t.Fatalf("Sleep() = %v, want %v", got, tc.want)
			}
		})
	}
}

// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"testing"
)

func TestNeedsSudo(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want bool
	}{
		"standard user": {
			env:  nil,
			want: true,
		},
		"termux by version": {
			env:  map[string]string{"TERMUX_VERSION": "0.118.0"},
			want: false,
		},
		"termux by prefix": {
			env:  map[string]string{"PREFIX": "/data/data/com.termux/files/usr"},
			want: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rt := &Runtime{
				Getenv: func(key string) string {
					if tc.env == nil {
						return ""
					}
					return tc.env[key]
				},
			}
			// Note: We can't easily mock os.Geteuid() without refactoring,
			// but we can test the Termux detection logic assuming Geteuid != 0.
			// Since this test likely runs as non-root in CI, it's fine.
			got := rt.NeedsSudo()
			if got != tc.want {
				t.Errorf("NeedsSudo() = %v, want %v", got, tc.want)
			}
		})
	}
}

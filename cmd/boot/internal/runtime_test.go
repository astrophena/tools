// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"os"
	"testing"
)

func TestNeedsSudo(t *testing.T) {
	t.Setenv("TERMUX_VERSION", "")
	t.Setenv("PREFIX", "")

	cases := map[string]struct {
		env  map[string]string
		want bool
	}{
		"standard_user": {
			env:  nil,
			want: os.Geteuid() != 0,
		},
		"termux_by_version": {
			env:  map[string]string{"TERMUX_VERSION": "0.118.0"},
			want: false,
		},
		"termux_by_prefix": {
			env:  map[string]string{"PREFIX": "/data/data/com.termux/files/usr"},
			want: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rt := &Runtime{
				Env: tc.env,
			}
			got := rt.NeedsSudo()
			if got != tc.want {
				t.Errorf("NeedsSudo() = %v, want %v", got, tc.want)
			}
		})
	}
}

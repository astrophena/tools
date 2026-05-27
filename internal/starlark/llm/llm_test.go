// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package llm

import (
	"testing"

	"go.astrophena.name/base/testutil"
)

func TestContentPartType(t *testing.T) {
	cases := map[string]struct {
		role string
		want string
	}{
		"assistant":    {role: "assistant", want: "output_text"},
		"legacy model": {role: "model", want: "output_text"},
		"user":         {role: "user", want: "input_text"},
		"system":       {role: "system", want: "input_text"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			testutil.AssertEqual(t, contentPartType(tc.role), tc.want)
		})
	}
}

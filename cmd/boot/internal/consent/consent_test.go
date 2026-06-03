// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package consent

import (
	"strings"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"
	"go.starlark.net/starlark"
)

func TestRequire(t *testing.T) {
	cases := map[string]struct {
		interactive bool
		input       string
		defaultYes  bool
		dryRun      bool
		want        boot.Result
	}{
		"dry run plans change": {
			dryRun: true,
			want:   boot.ResultChange,
		},
		"non-interactive stops task": {
			want: boot.ResultStop,
		},
		"accepted": {
			interactive: true,
			input:       "yes\n",
			want:        boot.ResultSkip,
		},
		"denied": {
			interactive: true,
			input:       "no\n",
			want:        boot.ResultStop,
		},
		"default accepted": {
			interactive: true,
			defaultYes:  true,
			input:       "\n",
			want:        boot.ResultSkip,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rt := &boot.Runtime{
				Stdin:       strings.NewReader(tc.input),
				Stdout:      new(strings.Builder),
				Interactive: tc.interactive,
			}
			task, thread := testutil.TaskThread("test")
			m := &impl{rt: rt}
			kwargs := []starlark.Tuple{{starlark.String("message"), starlark.String("Proceed?")}}
			if tc.defaultYes {
				kwargs = append(kwargs, starlark.Tuple{starlark.String("default"), starlark.True})
			}
			_, err := m.require(thread, starlark.NewBuiltin("consent.require", m.require), nil, kwargs)
			if err != nil {
				t.Fatal(err)
			}
			got, err := task.Actions[0].Apply(t.Context(), tc.dryRun)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("result = %s, want %s", got, tc.want)
			}
		})
	}
}

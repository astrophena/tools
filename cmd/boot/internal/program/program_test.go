// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package program

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"
	"go.starlark.net/starlark"
)

func TestUpdate(t *testing.T) {
	cases := map[string]struct {
		check     string
		dryRun    bool
		want      boot.Result
		wantApply bool
	}{
		"applies needed update": {
			check:     "true",
			want:      boot.ResultChange,
			wantApply: true,
		},
		"plans needed update": {
			check:  "true",
			dryRun: true,
			want:   boot.ResultChange,
		},
		"skips current program": {
			check: "false",
			want:  boot.ResultSkip,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			log := filepath.Join(t.TempDir(), "program.log")
			t.Setenv("PROGRAM_CHECK", tc.check)
			t.Setenv("PROGRAM_LOG", log)
			testutil.Commands(t, map[string]string{
				"updater": `#!/bin/sh
echo "$@" >> "$PROGRAM_LOG"
if [ "$2" = "-check" ]; then
	printf '%s\n' "$PROGRAM_CHECK"
fi
`,
			})

			h := testutil.NewTask(t, "test")
			m := new(impl)
			action := h.EmitOne("program.update", m.update, starlark.Tuple{starlark.NewList([]starlark.Value{
				starlark.String("updater"),
				starlark.String("update"),
			})}, nil)
			got, err := action.Apply(t.Context(), tc.dryRun)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("result = %s, want %s", got, tc.want)
			}
			output, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(string(output), "\nupdate\n"); got != tc.wantApply {
				t.Fatalf("apply call = %v, want %v:\n%s", got, tc.wantApply, output)
			}
		})
	}
}

func TestUpdateRejectsInvalidCheckResult(t *testing.T) {
	testutil.Commands(t, map[string]string{
		"updater": "#!/bin/sh\necho maybe\n",
	})
	h := testutil.NewTask(t, "test")
	m := new(impl)
	action := h.EmitOne("program.update", m.update, starlark.Tuple{starlark.NewList([]starlark.Value{
		starlark.String("updater"),
	})}, nil)
	_, err := action.Apply(t.Context(), true)
	if err == nil || !strings.Contains(err.Error(), `invalid update check result "maybe"`) {
		t.Fatalf("error = %v, want invalid check result", err)
	}
}

func TestUpdateReportsCommandFailures(t *testing.T) {
	cases := map[string]struct {
		script string
		want   string
	}{
		"check failure": {
			script: "#!/bin/sh\necho check failed >&2\nexit 1\n",
			want:   "check failed",
		},
		"apply failure": {
			script: `#!/bin/sh
if [ "$1" = "-check" ]; then
	echo true
	exit 0
fi
echo apply failed >&2
exit 1
`,
			want: "apply failed",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			testutil.Commands(t, map[string]string{"updater": tc.script})
			h := testutil.NewTask(t, "test")
			m := new(impl)
			action := h.EmitOne("program.update", m.update, starlark.Tuple{starlark.NewList([]starlark.Value{
				starlark.String("updater"),
			})}, nil)
			_, err := action.Apply(t.Context(), false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want mention %q", err, tc.want)
			}
		})
	}
}

func TestUpdateRejectsInvalidArgv(t *testing.T) {
	cases := map[string]struct {
		argv *starlark.List
		want string
	}{
		"empty list": {
			argv: starlark.NewList(nil),
			want: "argv cannot be empty",
		},
		"empty argument": {
			argv: starlark.NewList([]starlark.Value{starlark.String("")}),
			want: "argv[0] cannot be empty",
		},
		"non-string argument": {
			argv: starlark.NewList([]starlark.Value{starlark.MakeInt(1)}),
			want: "argv[0] is not a string",
		},
		"reserved flag": {
			argv: starlark.NewList([]starlark.Value{starlark.String("updater"), starlark.String("-check")}),
			want: "uses reserved flag -check",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			task := &boot.Task{ID: "test"}
			thread := &starlark.Thread{Name: "test"}
			boot.SetTask(thread, task)
			m := new(impl)
			_, err := m.update(thread, starlark.NewBuiltin("program.update", m.update), starlark.Tuple{tc.argv}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want mention %q", err, tc.want)
			}
		})
	}
}

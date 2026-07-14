// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package systemd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"
	"go.starlark.net/starlark"
)

func TestSystemUnitRequiresSudo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("sudo is not needed when running as root")
	}
	h := testutil.NewTask(t, "test")
	m := &impl{rt: &boot.Runtime{Getenv: func(string) string { return "" }}}
	action := h.EmitOne("systemd.system_unit", m.systemUnit, nil, []starlark.Tuple{
		{starlark.String("name"), starlark.String("sshd.service")},
		{starlark.String("enabled"), starlark.True},
	})
	if !action.RequiresSudo {
		t.Fatal("RequiresSudo is false, want true")
	}
}

func TestSystemctlQuietReturnsUnexpectedExitErrors(t *testing.T) {
	testutil.Commands(t, map[string]string{
		"systemctl": "#!/bin/sh\necho 'Failed to connect to bus' >&2\nexit 1\n",
	})
	ok, err := systemctlQuiet(t.Context(), &boot.Runtime{}, true, "is-active", "demo.service")
	if err == nil {
		t.Fatalf("systemctlQuiet = %v, nil; want error", ok)
	}
	if !strings.Contains(err.Error(), "Failed to connect to bus") {
		t.Fatalf("error = %v, want bus failure output", err)
	}
}

func TestUserUnit(t *testing.T) {
	cases := map[string]struct {
		needReload string
		dryRun     bool
		want       boot.Result
		wantReload bool
	}{
		"changes when reload needed": {
			needReload: "yes",
			want:       boot.ResultChange,
			wantReload: true,
		},
		"skips when current": {
			needReload: "no",
			dryRun:     true,
			want:       boot.ResultSkip,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			log := filepath.Join(t.TempDir(), "systemctl.log")
			testutil.Commands(t, map[string]string{
				"systemctl": `#!/bin/sh
echo "$@" >> "` + log + `"
case "$*" in
"--user show periodic.timer --property=NeedDaemonReload --value") echo ` + tc.needReload + `; exit 0 ;;
"--user is-enabled periodic.timer") echo enabled; exit 0 ;;
"--user is-active periodic.timer") echo active; exit 0 ;;
"--user daemon-reload"|"--user enable --now periodic.timer") exit 0 ;;
*) exit 1 ;;
esac
`,
			})

			h := testutil.NewTask(t, "test")
			m := &impl{}
			action := h.EmitOne("systemd.user_unit", m.userUnit, nil, []starlark.Tuple{
				{starlark.String("name"), starlark.String("periodic.timer")},
				{starlark.String("enabled"), starlark.True},
				{starlark.String("started"), starlark.True},
				{starlark.String("daemon_reload"), starlark.True},
			})
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
			mutated := strings.Contains(string(output), "--user daemon-reload") || strings.Contains(string(output), "--user enable --now")
			if mutated != tc.wantReload {
				t.Fatalf("mutating call = %v, want %v:\n%s", mutated, tc.wantReload, output)
			}
		})
	}
}

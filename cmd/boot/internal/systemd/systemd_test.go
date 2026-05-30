// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package systemd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"
	"go.starlark.net/starlark"
)

func TestUserUnitSkipsWhenCurrent(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "systemctl.log")
	testutil.WriteCommand(t, bin, "systemctl", `#!/bin/sh
echo "$@" >> "`+log+`"
case "$*" in
"--user show periodic.timer --property=NeedDaemonReload --value") echo no; exit 0 ;;
"--user is-enabled periodic.timer") echo enabled; exit 0 ;;
"--user is-active periodic.timer") echo active; exit 0 ;;
*) exit 1 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	task, thread := testutil.TaskThread("test")
	m := &impl{}
	_, err := m.userUnit(thread, starlark.NewBuiltin("systemd.user_unit", m.userUnit), nil, []starlark.Tuple{
		{starlark.String("name"), starlark.String("periodic.timer")},
		{starlark.String("enabled"), starlark.True},
		{starlark.String("started"), starlark.True},
		{starlark.String("daemon_reload"), starlark.True},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := task.Actions[0].Apply(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if got != boot.ResultSkip {
		t.Fatalf("result = %s, want %s", got, boot.ResultSkip)
	}
	out, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "daemon-reload") || strings.Contains(string(out), "enable --now") {
		t.Fatalf("dry run invoked changing command:\n%s", out)
	}
}

func TestUserUnitChangesWhenReloadNeeded(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "systemctl.log")
	testutil.WriteCommand(t, bin, "systemctl", `#!/bin/sh
echo "$@" >> "`+log+`"
case "$*" in
"--user show periodic.timer --property=NeedDaemonReload --value") echo yes; exit 0 ;;
"--user is-enabled periodic.timer") echo enabled; exit 0 ;;
"--user is-active periodic.timer") echo active; exit 0 ;;
"--user daemon-reload") exit 0 ;;
"--user enable --now periodic.timer") exit 0 ;;
*) exit 1 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	task, thread := testutil.TaskThread("test")
	m := &impl{}
	_, err := m.userUnit(thread, starlark.NewBuiltin("systemd.user_unit", m.userUnit), nil, []starlark.Tuple{
		{starlark.String("name"), starlark.String("periodic.timer")},
		{starlark.String("enabled"), starlark.True},
		{starlark.String("started"), starlark.True},
		{starlark.String("daemon_reload"), starlark.True},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := task.Actions[0].Apply(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if got != boot.ResultChange {
		t.Fatalf("result = %s, want %s", got, boot.ResultChange)
	}
	out, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "--user daemon-reload") {
		t.Fatalf("daemon-reload was not invoked:\n%s", out)
	}
}

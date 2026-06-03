// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package flatpak

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"
	"go.starlark.net/starlark"
)

func TestUpdateSkipsWithoutUpdates(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "flatpak.log")
	testutil.WriteCommand(t, bin, "flatpak", `#!/bin/sh
echo "$@" >> "`+log+`"
case "$*" in
"remote-ls --updates") exit 0 ;;
*) exit 1 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	result := runUpdateAction(t, false)
	if result != boot.ResultSkip {
		t.Fatalf("result = %s, want %s", result, boot.ResultSkip)
	}
	out, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "update -y") {
		t.Fatalf("flatpak update was invoked despite no pending updates:\n%s", out)
	}
}

func TestUpdateAppliesWhenUpdatesExist(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "flatpak.log")
	testutil.WriteCommand(t, bin, "flatpak", `#!/bin/sh
echo "$@" >> "`+log+`"
case "$*" in
"remote-ls --updates") echo org.example.App; exit 0 ;;
"update -y") exit 0 ;;
*) exit 1 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	result := runUpdateAction(t, false)
	if result != boot.ResultChange {
		t.Fatalf("result = %s, want %s", result, boot.ResultChange)
	}
	out, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "update -y") {
		t.Fatalf("flatpak update was not invoked:\n%s", out)
	}
}

func TestUpdateDryRunDoesNotApply(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "flatpak.log")
	testutil.WriteCommand(t, bin, "flatpak", `#!/bin/sh
echo "$@" >> "`+log+`"
case "$*" in
"remote-ls --updates") echo org.example.App; exit 0 ;;
*) exit 1 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	result := runUpdateAction(t, true)
	if result != boot.ResultChange {
		t.Fatalf("result = %s, want %s", result, boot.ResultChange)
	}
	out, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "update -y") {
		t.Fatalf("flatpak update was invoked in dry run:\n%s", out)
	}
}

func runUpdateAction(t *testing.T, dryRun bool) boot.Result {
	t.Helper()
	task, thread := testutil.TaskThread("test")
	m := &impl{}
	_, err := m.update(thread, starlark.NewBuiltin("flatpak.update", m.update), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Actions) != 1 {
		t.Fatalf("got %d actions, want 1", len(task.Actions))
	}
	result, err := task.Actions[0].Apply(t.Context(), dryRun)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

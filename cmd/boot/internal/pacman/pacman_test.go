// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package pacman

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"
	"go.starlark.net/starlark"
)

func TestCheckOrphansSkipsWhenNone(t *testing.T) {
	bin := t.TempDir()
	testutil.WriteCommand(t, bin, "pacman", `#!/bin/sh
case "$*" in
"-Qtdq") exit 1 ;;
*) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, warnings := runOrphansAction(t)
	if result != boot.ResultSkip {
		t.Fatalf("result = %s, want %s", result, boot.ResultSkip)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %q, want none", warnings)
	}
}

func TestCheckOrphansWarnsWhenPresent(t *testing.T) {
	bin := t.TempDir()
	testutil.WriteCommand(t, bin, "pacman", `#!/bin/sh
case "$*" in
"-Qtdq") printf 'oldlib\nunused\n'; exit 0 ;;
*) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, warnings := runOrphansAction(t)
	if result != boot.ResultSkip {
		t.Fatalf("result = %s, want %s", result, boot.ResultSkip)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "orphaned packages found") || !strings.Contains(warnings[0], "oldlib") {
		t.Fatalf("warning missing orphan details: %q", warnings)
	}
}

func TestPacnewFilesSorted(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"z.conf.pacnew", "a.conf.pacnew", "sub/b.conf.pacnew"} {
		if err := os.WriteFile(filepath.Join(root, "etc", name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := pacnewFiles(filepath.Join(root, "etc"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || !strings.HasSuffix(files[0], "a.conf.pacnew") || !strings.HasSuffix(files[2], "z.conf.pacnew") {
		t.Fatalf("files = %v, want sorted pacnew files", files)
	}
}

func runOrphansAction(t *testing.T) (boot.Result, []string) {
	t.Helper()
	task, thread := testutil.TaskThread("test")
	m := &impl{rt: &boot.Runtime{}}
	_, err := m.checkOrphans(thread, starlark.NewBuiltin("pacman.check_orphans", m.checkOrphans), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Actions) != 1 {
		t.Fatalf("got %d actions, want 1", len(task.Actions))
	}
	var warnings []string
	ctx := boot.WithWarningSink(t.Context(), func(message string) {
		warnings = append(warnings, message)
	})
	result, err := task.Actions[0].Apply(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	return result, warnings
}

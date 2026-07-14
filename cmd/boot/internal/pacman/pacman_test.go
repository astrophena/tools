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
)

func TestCheckOrphansSkipsWhenNone(t *testing.T) {
	testutil.Commands(t, map[string]string{"pacman": `#!/bin/sh
case "$*" in
"-Qtdq") exit 1 ;;
*) exit 2 ;;
esac
`})

	result, warnings := runOrphansAction(t)
	if result != boot.ResultSkip {
		t.Fatalf("result = %s, want %s", result, boot.ResultSkip)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %q, want none", warnings)
	}
}

func TestCheckOrphansWarnsWhenPresent(t *testing.T) {
	testutil.Commands(t, map[string]string{"pacman": `#!/bin/sh
case "$*" in
"-Qtdq") printf 'oldlib\nunused\n'; exit 0 ;;
*) exit 2 ;;
esac
`})

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
	h := testutil.NewTask(t, "test")
	m := &impl{rt: &boot.Runtime{}}
	action := h.EmitOne("pacman.check_orphans", m.checkOrphans, nil, nil)
	result, warnings, err := testutil.RunAction(t.Context(), action, false)
	if err != nil {
		t.Fatal(err)
	}
	return result, warnings
}

// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package packages

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func TestCheckExplicitPackages(t *testing.T) {
	testutil.Commands(t, map[string]string{
		"apt-mark": `#!/bin/sh
test "$*" = "showmanual" || exit 2
printf 'known\ncurl\n'
`,
		"pacman": `#!/bin/sh
test "$*" = "-Qqen" || exit 2
printf 'zlib\nknown\ngit\n'
`,
	})
	rt := &boot.Runtime{Env: map[string]string{"BOOT_PACKAGE_MANAGER": "apt"}}
	cases := map[string]struct {
		module *starlarkstruct.Module
		want   string
	}{
		"configured_manager": {Module(rt), "  - curl"},
		"fixed_pacman":       {PacmanModule(rt), "  - git\n  - zlib"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := testutil.NewTask(t, "test")
			packages := starlark.NewList([]starlark.Value{starlark.String("known")})
			builtin := tc.module.Members["check_explicit_packages"]
			if _, err := starlark.Call(h.Thread, builtin, starlark.Tuple{packages}, nil); err != nil {
				t.Fatal(err)
			}
			if len(h.Actions) != 1 {
				t.Fatalf("actions = %d, want 1", len(h.Actions))
			}
			result, warnings, err := testutil.RunAction(t.Context(), h.Actions[0], true)
			if err != nil {
				t.Fatal(err)
			}
			if result != boot.ResultSkip {
				t.Fatalf("result = %s, want %s", result, boot.ResultSkip)
			}
			want := "explicit packages missing from recipe:\n" + tc.want
			if len(warnings) != 1 || warnings[0] != want {
				t.Fatalf("warnings = %q, want %q", warnings, want)
			}
		})
	}
}

func TestCheckOrphansSkipsWhenNone(t *testing.T) {
	testutil.Commands(t, map[string]string{
		"pacman": `#!/bin/sh
case "$*" in
"-Qtdq") exit 1 ;;
*) exit 2 ;;
esac
`,
	})

	result, warnings := runOrphansAction(t)
	if result != boot.ResultSkip {
		t.Fatalf("result = %s, want %s", result, boot.ResultSkip)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %q, want none", warnings)
	}
}

func TestCheckOrphansWarnsWhenPresent(t *testing.T) {
	testutil.Commands(t, map[string]string{
		"pacman": `#!/bin/sh
case "$*" in
"-Qtdq") printf 'oldlib\nunused\n'; exit 0 ;;
*) exit 2 ;;
esac
`,
	})

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
	action := h.EmitOne("pacman.check_orphans", checkOrphans, nil, nil)
	result, warnings, err := testutil.RunAction(t.Context(), action, false)
	if err != nil {
		t.Fatal(err)
	}
	return result, warnings
}

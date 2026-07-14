// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package rescue

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"
	"go.starlark.net/starlark"
)

func TestUpdateDescribesPlannedRescueImageBuild(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "rescue")
	esp := filepath.Join(root, "efi")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(esp, 0o755); err != nil {
		t.Fatal(err)
	}
	oldImage := filepath.Join(esp, "arch-linux-rescue_202601010102.efi")
	writeFile(t, oldImage)
	oldTime := time.Now().AddDate(0, -1, 0)
	if err := os.Chtimes(oldImage, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	h := testutil.NewTask(t, "test")
	m := &impl{rt: &boot.Runtime{Root: root, Home: filepath.Join(root, "home"), Getenv: func(string) string { return "termux" }}}

	action := h.EmitOne("rescue.update", m.update, nil, []starlark.Tuple{
		{starlark.String("source"), starlark.String("rescue")},
		{starlark.String("esp_dir"), starlark.String("efi")},
		{starlark.String("keep"), starlark.MakeInt(2)},
	})
	result, err := action.Apply(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if result != boot.ResultChange {
		t.Fatalf("result = %s, want %s", result, boot.ResultChange)
	}
	description := action.Describe()
	for _, want := range []string{
		"update rescue image from " + source + " to " + esp + " (keep 2)",
		"would build and install",
		"latest installed image is arch-linux-rescue_202601010102.efi from",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("description does not contain %q:\n%s", want, description)
		}
	}
}

func TestRescueOutputBuildsGroupsTimestampedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "arch-linux-rescue_202605010102.efi"))
	writeFile(t, filepath.Join(dir, "arch-linux-rescue_202605010102.efi.sig"))
	writeFile(t, filepath.Join(dir, "arch-linux-rescue_202604010102.efi"))
	writeFile(t, filepath.Join(dir, "unrelated.txt"))

	builds, err := rescueOutputBuilds(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(builds) != 2 {
		t.Fatalf("got %d builds, want 2", len(builds))
	}
	if builds[0].stamp != "202605010102" {
		t.Fatalf("first stamp = %s, want 202605010102", builds[0].stamp)
	}
	if len(builds[0].paths) != 2 {
		t.Fatalf("first build has %d paths, want 2", len(builds[0].paths))
	}
}

func TestPruneOutputKeepsNewestBuilds(t *testing.T) {
	root := t.TempDir()
	testutil.Commands(t, map[string]string{"rm": `#!/bin/sh
for arg in "$@"; do
	if [ "$arg" != "-f" ]; then
		/bin/rm -f "$arg"
	fi
done
`})

	files := []string{
		"arch-linux-rescue_202605010102.efi",
		"arch-linux-rescue_202605010102.efi.sig",
		"arch-linux-rescue_202604010102.efi",
		"arch-linux-rescue_202603010102.efi",
		"unrelated.txt",
	}
	for _, name := range files {
		writeFile(t, filepath.Join(root, name))
	}

	if err := pruneOutput(t.Context(), &boot.Runtime{Getenv: func(string) string { return "termux" }}, root, 2); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"arch-linux-rescue_202605010102.efi",
		"arch-linux-rescue_202605010102.efi.sig",
		"arch-linux-rescue_202604010102.efi",
		"unrelated.txt",
	} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("%s was removed: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "arch-linux-rescue_202603010102.efi")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("old build still exists: %v", err)
	}
}

func TestPruneOutputAllowsKeepLargerThanBuildCount(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "arch-linux-rescue_202605010102.efi"))

	if err := pruneOutput(t.Context(), &boot.Runtime{Getenv: func(string) string { return "termux" }}, root, 3); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, "arch-linux-rescue_202605010102.efi")); err != nil {
		t.Fatal(err)
	}
}

func TestPruneAllowsKeepLargerThanImageCount(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "arch-linux-rescue_202605010102.efi"))

	if err := prune(t.Context(), &boot.Runtime{Getenv: func(string) string { return "termux" }}, root, 3); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(root, "arch-linux-rescue_202605010102.efi")); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
}

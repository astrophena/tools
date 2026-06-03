// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package rescue

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"
)

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
	bin := t.TempDir()
	testutil.WriteCommand(t, bin, "rm", `#!/bin/sh
for arg in "$@"; do
	if [ "$arg" != "-f" ]; then
		/bin/rm -f "$arg"
	fi
done
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

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

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
}

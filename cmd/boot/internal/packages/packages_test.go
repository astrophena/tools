// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package packages

import (
	"context"
	"os"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"
)

func TestPackageManagerAptMissing(t *testing.T) {
	bin := t.TempDir()
	testutil.WriteCommand(t, bin, "dpkg-query", `#!/bin/sh
for arg in "$@"; do
    case "$arg" in
    installed) echo "installed install ok installed" ;;
    missing) ;;
    esac
done
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	rt := &boot.Runtime{Getenv: os.Getenv}
	pm, err := packageManagerByName(rt, "apt")
	if err != nil {
		t.Fatal(err)
	}
	missing, err := pm.missing(context.Background(), []string{"installed", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != "missing" {
		t.Fatalf("missing = %v, want [missing]", missing)
	}
}

func TestPackageManagerPacmanMissing(t *testing.T) {
	bin := t.TempDir()
	testutil.WriteCommand(t, bin, "pacman", `#!/bin/sh
for arg in "$@"; do
    case "$arg" in
    missing) echo "missing"; exit 127 ;;
    esac
done
exit 0
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	rt := &boot.Runtime{Getenv: os.Getenv}
	pm, err := packageManagerByName(rt, "pacman")
	if err != nil {
		t.Fatal(err)
	}
	missing, err := pm.missing(context.Background(), []string{"installed", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != "missing" {
		t.Fatalf("missing = %v, want [missing]", missing)
	}
}

func TestPackageManagerAptUpdate(t *testing.T) {
	bin := t.TempDir()
	testutil.WriteCommand(t, bin, "sh", `#!/bin/sh
case "$2" in
"apt update && apt upgrade -y") exit 0 ;;
"sudo apt update && sudo apt upgrade -y") exit 0 ;;
*) exit 1 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	rt := &boot.Runtime{Getenv: os.Getenv}
	pm, err := packageManagerByName(rt, "apt")
	if err != nil {
		t.Fatal(err)
	}
	if err := pm.update(context.Background()); err != nil {
		t.Fatalf("update failed: %v", err)
	}
}

func TestPackageManagerPacmanUpdate(t *testing.T) {
	bin := t.TempDir()
	testutil.WriteCommand(t, bin, "sudo", `#!/bin/sh
if [ "$1" = "pacman" ] && [ "$2" = "-Syu" ] && [ "$3" = "--noconfirm" ]; then
	exit 0
fi
exit 1
`)
	testutil.WriteCommand(t, bin, "pacman", `#!/bin/sh
if [ "$1" = "-Syu" ] && [ "$2" = "--noconfirm" ]; then
	exit 0
fi
exit 1
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	rt := &boot.Runtime{Getenv: os.Getenv}
	pm, err := packageManagerByName(rt, "pacman")
	if err != nil {
		t.Fatal(err)
	}
	if err := pm.update(context.Background()); err != nil {
		t.Fatalf("update failed: %v", err)
	}
}

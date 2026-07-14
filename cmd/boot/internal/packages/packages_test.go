// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package packages

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"
	"go.starlark.net/starlark"
)

func TestInstallRequiresSudo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("sudo is not needed when running as root")
	}
	h := testutil.NewTask(t, "test")
	mod := &module{manager: "apt"}
	m := &impl{rt: &boot.Runtime{Getenv: func(string) string { return "" }}, mod: mod}
	packages := starlark.NewList([]starlark.Value{starlark.String("curl")})
	action := h.EmitOne("pkg.install", m.install, nil, []starlark.Tuple{
		{starlark.String("packages"), packages},
	})
	if !action.RequiresSudo {
		t.Fatal("RequiresSudo is false, want true")
	}
}

func TestPackageManagerAptMissing(t *testing.T) {
	testutil.Commands(t, map[string]string{"dpkg-query": `#!/bin/sh
for arg in "$@"; do
    case "$arg" in
    installed) echo "installed install ok installed" ;;
    missing) ;;
    esac
done
`})

	rt := &boot.Runtime{Getenv: os.Getenv}
	pm, err := packageManagerByName(rt, "apt")
	if err != nil {
		t.Fatal(err)
	}
	missing, err := pm.missing(t.Context(), []string{"installed", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != "missing" {
		t.Fatalf("missing = %v, want [missing]", missing)
	}
}

func TestPackageManagerPacmanMissing(t *testing.T) {
	testutil.Commands(t, map[string]string{"pacman": `#!/bin/sh
for arg in "$@"; do
    case "$arg" in
    missing) echo "missing"; exit 127 ;;
    esac
done
exit 0
`})

	rt := &boot.Runtime{Getenv: os.Getenv}
	pm, err := packageManagerByName(rt, "pacman")
	if err != nil {
		t.Fatal(err)
	}
	missing, err := pm.missing(t.Context(), []string{"installed", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != "missing" {
		t.Fatalf("missing = %v, want [missing]", missing)
	}
}

func TestPackageManagerAptUpdate(t *testing.T) {
	testutil.Commands(t, map[string]string{"sh": `#!/bin/sh
case "$2" in
"apt update && apt upgrade -y") exit 0 ;;
"sudo apt update && sudo apt upgrade -y") exit 0 ;;
*) exit 1 ;;
esac
`})

	rt := &boot.Runtime{Getenv: os.Getenv}
	pm, err := packageManagerByName(rt, "apt")
	if err != nil {
		t.Fatal(err)
	}
	if err := pm.update(t.Context()); err != nil {
		t.Fatalf("update failed: %v", err)
	}
}

func TestPackageManagerPacmanUpdates(t *testing.T) {
	rt, cache := newPacmanRuntime(t, map[string]string{
		"fakeroot": `#!/bin/sh
if [ "$1" != "--" ] || [ "$2" != "pacman" ] || [ "$3" != "-Sy" ]; then
	exit 1
fi
exit 0
`,
		"pacman": `#!/bin/sh
case "$1" in
-Qu)
	echo "linux 1-1 -> 1-2"
	echo "ignored 1-1 -> 1-2 [ignored]"
	exit 0 ;;
*) exit 1 ;;
esac
`,
	})

	updates, err := pacmanUpdates(t.Context(), rt)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 || updates[0] != "linux" {
		t.Fatalf("updates = %v, want [linux]", updates)
	}
	if _, err := os.Lstat(filepath.Join(cache, "boot", "pacman", "local")); err != nil {
		t.Fatalf("local database link was not created: %v", err)
	}
}

func TestPackageManagerPacmanUpdatesNone(t *testing.T) {
	rt, _ := newPacmanRuntime(t, map[string]string{
		"pacman": `#!/bin/sh
case "$1" in
-Qu) exit 1 ;;
*) exit 1 ;;
esac
`,
	})

	updates, err := pacmanUpdates(t.Context(), rt)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 0 {
		t.Fatalf("updates = %v, want none", updates)
	}
}

func TestPacmanRebootRequired(t *testing.T) {
	cases := map[string]struct {
		packages []string
		output   string
		exitCode int
		want     []string
		wantErr  bool
	}{
		"kernel module owners": {
			packages: []string{"linux", "git", "nvidia-open"},
			output: strings.Join([]string{
				"linux /usr/lib/modules/6.15.1-arch1-1/kernel/fs/btrfs/btrfs.ko.zst",
				"git /usr/bin/git",
				"nvidia-open /usr/lib/modules/6.15.1-arch1-1/extramodules/nvidia.ko.zst",
			}, "\n"),
			want: []string{"linux", "nvidia-open"},
		},
		"duplicate module paths": {
			packages: []string{"linux"},
			output: strings.Join([]string{
				"linux /usr/lib/modules/",
				"linux /usr/lib/modules/6.15.1-arch1-1/vmlinuz",
			}, "\n"),
			want: []string{"linux"},
		},
		"ordinary packages": {
			packages: []string{"git"},
			output:   "git /usr/bin/git",
		},
		"query failure": {
			packages: []string{"linux"},
			exitCode: 1,
			wantErr:  true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			testutil.Commands(t, map[string]string{
				"pacman": fmt.Sprintf("#!/bin/sh\nprintf '%%b' %q\nexit %d\n", tc.output, tc.exitCode),
			})

			got, err := pacmanRebootRequired(t.Context(), tc.packages)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error %v", err, tc.wantErr)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("packages = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUpdateDescribesPacmanUpdatesInPlan(t *testing.T) {
	rt, _ := newPacmanRuntime(t, map[string]string{
		"pacman": `#!/bin/sh
case "$1" in
-Qu)
	echo "linux 1-1 -> 1-2"
	echo "git 1-1 -> 1-2"
	exit 0 ;;
-Ql)
	echo "linux /usr/lib/modules/6.15.1-arch1-1/vmlinuz"
	echo "git /usr/bin/git"
	exit 0 ;;
*) exit 1 ;;
esac
`,
	})
	h := testutil.NewTask(t, "test")
	m := &impl{rt: rt, mod: &module{manager: "pacman"}}
	action := h.EmitOne("pkg.update", m.update, nil, nil)
	result, warnings, err := testutil.RunAction(t.Context(), action, true)
	if err != nil {
		t.Fatal(err)
	}
	if result != boot.ResultChange {
		t.Fatalf("result = %s, want %s", result, boot.ResultChange)
	}
	if got, want := action.Describe(), "update system with pacman: would update linux, git"; got != want {
		t.Fatalf("description = %q, want %q", got, want)
	}
	if got, want := warnings, []string{"reboot will be required after updating linux"}; !slices.Equal(got, want) {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
}

func newPacmanRuntime(t *testing.T, scripts map[string]string) (*boot.Runtime, string) {
	t.Helper()
	systemDB := filepath.Join(t.TempDir(), "pacman")
	if err := os.MkdirAll(filepath.Join(systemDB, "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	commands := map[string]string{
		"fakeroot":    "#!/bin/sh\nexit 0\n",
		"pacman-conf": "#!/bin/sh\necho " + systemDB + "\n",
	}
	maps.Copy(commands, scripts)
	testutil.Commands(t, commands)
	cache := t.TempDir()
	return &boot.Runtime{Getenv: func(key string) string {
		if key == "XDG_CACHE_HOME" {
			return cache
		}
		return os.Getenv(key)
	}}, cache
}

func TestUpdateReportsRebootAfterSuccessfulApply(t *testing.T) {
	cases := map[string]struct {
		updates        []string
		rebootPackages []string
		updateErr      error
		wantWarning    string
	}{
		"kernel modules updated": {
			updates:        []string{"linux", "git", "nvidia-open"},
			rebootPackages: []string{"linux", "nvidia-open"},
			wantWarning:    "machine needs rebooting after updating linux, nvidia-open",
		},
		"ordinary package updated": {
			updates: []string{"git"},
		},
		"update failed": {
			updates:        []string{"linux"},
			rebootPackages: []string{"linux"},
			updateErr:      errors.New("update failed"),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := testutil.NewTask(t, "test")
			m := &impl{rt: &boot.Runtime{}, mod: &module{}}
			m.addUpdateActions(h.Thread, packageManager{
				name: "pacman",
				updates: func(context.Context) ([]string, error) {
					return tc.updates, nil
				},
				rebootRequired: func(context.Context, []string) ([]string, error) {
					return tc.rebootPackages, nil
				},
				update: func(context.Context) error {
					return tc.updateErr
				},
			})
			if len(h.Task.Actions) != 1 {
				t.Fatalf("actions = %d, want 1", len(h.Task.Actions))
			}
			result, warnings, err := testutil.RunAction(t.Context(), h.Task.Actions[0], false)
			if !errors.Is(err, tc.updateErr) {
				t.Fatalf("update error = %v, want %v", err, tc.updateErr)
			}
			if result != boot.ResultChange {
				t.Fatalf("update result = %s, want %s", result, boot.ResultChange)
			}
			if got := strings.Join(warnings, "\n"); got != tc.wantWarning {
				t.Fatalf("warning = %q, want %q", got, tc.wantWarning)
			}
		})
	}
}

func TestPackageManagerPacmanUpdate(t *testing.T) {
	testutil.Commands(t, map[string]string{
		"sudo": `#!/bin/sh
if [ "$1" = "pacman" ] && [ "$2" = "-Syu" ] && [ "$3" = "--noconfirm" ]; then
	exit 0
fi
exit 1
`,
		"pacman": `#!/bin/sh
if [ "$1" = "-Syu" ] && [ "$2" = "--noconfirm" ]; then
	exit 0
fi
exit 1
`,
	})

	rt := &boot.Runtime{Getenv: os.Getenv}
	pm, err := packageManagerByName(rt, "pacman")
	if err != nil {
		t.Fatal(err)
	}
	if err := pm.update(t.Context()); err != nil {
		t.Fatalf("update failed: %v", err)
	}
}

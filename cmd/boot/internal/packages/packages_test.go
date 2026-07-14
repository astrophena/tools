// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package packages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	task, thread := testutil.TaskThread("test")
	mod := &module{manager: "apt"}
	m := &impl{rt: &boot.Runtime{Getenv: func(string) string { return "" }}, mod: mod}
	packages := starlark.NewList([]starlark.Value{starlark.String("curl")})
	_, err := m.install(thread, starlark.NewBuiltin("pkg.install", m.install), nil, []starlark.Tuple{
		{starlark.String("packages"), packages},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !task.Actions[0].RequiresSudo {
		t.Fatal("RequiresSudo is false, want true")
	}
}

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
	missing, err := pm.missing(t.Context(), []string{"installed", "missing"})
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
	missing, err := pm.missing(t.Context(), []string{"installed", "missing"})
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
	if err := pm.update(t.Context()); err != nil {
		t.Fatalf("update failed: %v", err)
	}
}

func TestPackageManagerPacmanUpdates(t *testing.T) {
	bin := t.TempDir()
	systemDB := filepath.Join(t.TempDir(), "pacman")
	if err := os.MkdirAll(filepath.Join(systemDB, "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.WriteCommand(t, bin, "pacman-conf", "#!/bin/sh\necho "+systemDB+"\n")
	testutil.WriteCommand(t, bin, "fakeroot", `#!/bin/sh
if [ "$1" != "--" ] || [ "$2" != "pacman" ] || [ "$3" != "-Sy" ]; then
	exit 1
fi
exit 0
`)
	testutil.WriteCommand(t, bin, "pacman", `#!/bin/sh
case "$1" in
-Qu)
	echo "linux 1-1 -> 1-2"
	echo "ignored 1-1 -> 1-2 [ignored]"
	exit 0 ;;
*) exit 1 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cache := t.TempDir()
	rt := &boot.Runtime{Getenv: func(key string) string {
		if key == "XDG_CACHE_HOME" {
			return cache
		}
		return os.Getenv(key)
	}}

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
	bin := t.TempDir()
	systemDB := filepath.Join(t.TempDir(), "pacman")
	if err := os.MkdirAll(filepath.Join(systemDB, "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.WriteCommand(t, bin, "pacman-conf", "#!/bin/sh\necho "+systemDB+"\n")
	testutil.WriteCommand(t, bin, "fakeroot", "#!/bin/sh\nexit 0\n")
	testutil.WriteCommand(t, bin, "pacman", `#!/bin/sh
case "$1" in
-Qu) exit 1 ;;
*) exit 1 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cache := t.TempDir()
	rt := &boot.Runtime{Getenv: func(key string) string {
		if key == "XDG_CACHE_HOME" {
			return cache
		}
		return os.Getenv(key)
	}}

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
			bin := t.TempDir()
			testutil.WriteCommand(t, bin, "pacman", fmt.Sprintf("#!/bin/sh\nprintf '%%b' %q\nexit %d\n", tc.output, tc.exitCode))
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

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
	bin := t.TempDir()
	systemDB := filepath.Join(t.TempDir(), "pacman")
	if err := os.MkdirAll(filepath.Join(systemDB, "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.WriteCommand(t, bin, "pacman-conf", "#!/bin/sh\necho "+systemDB+"\n")
	testutil.WriteCommand(t, bin, "fakeroot", "#!/bin/sh\nexit 0\n")
	testutil.WriteCommand(t, bin, "pacman", `#!/bin/sh
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
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cache := t.TempDir()
	rt := &boot.Runtime{Getenv: func(key string) string {
		if key == "XDG_CACHE_HOME" {
			return cache
		}
		return os.Getenv(key)
	}}
	task, thread := testutil.TaskThread("test")
	m := &impl{rt: rt, mod: &module{manager: "pacman"}}

	_, err := m.update(thread, starlark.NewBuiltin("pkg.update", m.update), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Actions) != 2 {
		t.Fatalf("actions = %d, want 2", len(task.Actions))
	}
	result, err := task.Actions[0].Apply(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if result != boot.ResultChange {
		t.Fatalf("result = %s, want %s", result, boot.ResultChange)
	}
	if got, want := task.Actions[0].Describe(), "update system with pacman: would update linux, git"; got != want {
		t.Fatalf("description = %q, want %q", got, want)
	}
	result, err = task.Actions[1].Apply(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if result != boot.ResultWarn {
		t.Fatalf("reboot result = %s, want %s", result, boot.ResultWarn)
	}
	if got, want := task.Actions[1].Describe(), "reboot will be required after updating linux"; got != want {
		t.Fatalf("reboot description = %q, want %q", got, want)
	}
}

func TestUpdateReportsRebootAfterSuccessfulApply(t *testing.T) {
	cases := map[string]struct {
		updates          []string
		rebootPackages   []string
		updateErr        error
		wantRebootResult boot.Result
		wantOutput       string
	}{
		"kernel modules updated": {
			updates:          []string{"linux", "git", "nvidia-open"},
			rebootPackages:   []string{"linux", "nvidia-open"},
			wantRebootResult: boot.ResultWarn,
			wantOutput:       "machine needs rebooting after updating linux, nvidia-open\n",
		},
		"ordinary package updated": {
			updates:          []string{"git"},
			wantRebootResult: boot.ResultSkip,
		},
		"update failed": {
			updates:          []string{"linux"},
			rebootPackages:   []string{"linux"},
			updateErr:        errors.New("update failed"),
			wantRebootResult: boot.ResultSkip,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			task, thread := testutil.TaskThread("test")
			m := &impl{rt: &boot.Runtime{Stdout: &out}, mod: &module{}}
			m.addUpdateActions(thread, packageManager{
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
			if len(task.Actions) != 2 {
				t.Fatalf("actions = %d, want 2", len(task.Actions))
			}

			result, err := task.Actions[0].Apply(t.Context(), false)
			if !errors.Is(err, tc.updateErr) {
				t.Fatalf("update error = %v, want %v", err, tc.updateErr)
			}
			if result != boot.ResultChange {
				t.Fatalf("update result = %s, want %s", result, boot.ResultChange)
			}

			result, err = task.Actions[1].Apply(t.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			if result != tc.wantRebootResult {
				t.Fatalf("reboot result = %s, want %s", result, tc.wantRebootResult)
			}
			if got := out.String(); got != tc.wantOutput {
				t.Fatalf("output = %q, want %q", got, tc.wantOutput)
			}
		})
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
	if err := pm.update(t.Context()); err != nil {
		t.Fatalf("update failed: %v", err)
	}
}

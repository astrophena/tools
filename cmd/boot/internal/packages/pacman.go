// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package packages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// PacmanModule returns the Starlark pacman module.
func PacmanModule(rt *boot.Runtime) *starlarkstruct.Module {
	m := &module{rt: rt, managerName: "pacman"}
	return &starlarkstruct.Module{
		Name: "pacman",
		Members: starlark.StringDict{
			"check_explicit_packages": boot.TaskBuiltin("pacman.check_explicit_packages", m.checkExplicitPackages),
			"check_orphans":           boot.TaskBuiltin("pacman.check_orphans", checkOrphans),
			"check_pacnew":            boot.TaskBuiltin("pacman.check_pacnew", m.checkPacnew),
		},
	}
}

func checkOrphans(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(b.Name(), args, kwargs); err != nil {
		return nil, err
	}
	boot.AddAction(thread, boot.Action{
		Title:   "pacman orphans",
		Summary: "check pacman orphaned packages",
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			cmd := exec.CommandContext(ctx, "pacman", "-Qtdq")
			out, err := cmd.Output()
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
					return boot.ResultSkip, nil
				}
				return "", err
			}
			orphans := strings.Fields(string(out))
			if len(orphans) == 0 {
				return boot.ResultSkip, nil
			}
			boot.Warn(ctx, fmt.Sprintf(
				"orphaned packages found:\n%s\nremove them with: sudo pacman -Rns $(pacman -Qtdq)",
				boot.BulletList(orphans),
			))
			return boot.ResultSkip, nil
		},
	})
	return starlark.None, nil
}

func (m *module) checkPacnew(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var managedEtc string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "managed_etc", &managedEtc); err != nil {
		return nil, err
	}
	managedEtc = m.rt.ResolveSource(managedEtc)

	boot.AddAction(thread, boot.Action{
		Title:   "pacman .pacnew",
		Summary: "check pacman .pacnew files",
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			files, err := pacnewFiles("/etc")
			if err != nil {
				return "", err
			}
			if len(files) == 0 {
				return boot.ResultSkip, nil
			}
			var managed, unmanaged []string
			for _, path := range files {
				original := strings.TrimSuffix(path, ".pacnew")
				rel, err := filepath.Rel("/etc", original)
				if err != nil {
					unmanaged = append(unmanaged, path)
					continue
				}
				if _, err := os.Stat(filepath.Join(managedEtc, rel)); err == nil {
					managed = append(managed, path)
				} else {
					unmanaged = append(unmanaged, path)
				}
			}

			var buf bytes.Buffer
			fmt.Fprintln(&buf, ".pacnew files found")
			if len(managed) > 0 {
				fmt.Fprintln(&buf, "managed by recipe:")
				fmt.Fprintln(&buf, boot.BulletList(managed))
				fmt.Fprintln(&buf, pacnewDiffs(ctx, managed))
			}
			if len(unmanaged) > 0 {
				fmt.Fprintln(&buf, "unmanaged:")
				fmt.Fprintln(&buf, boot.BulletList(unmanaged))
				fmt.Fprintln(&buf, pacnewDiffs(ctx, unmanaged))
			}
			boot.Warn(ctx, buf.String())
			return boot.ResultSkip, nil
		},
	})
	return starlark.None, nil
}

func pacnewFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() && strings.HasSuffix(path, ".pacnew") {
			files = append(files, path)
		}
		return nil
	})
	slices.Sort(files)
	return files, err
}

func pacnewDiffs(ctx context.Context, files []string) string {
	const maxLines = 30
	var buf strings.Builder
	for _, path := range files {
		original := strings.TrimSuffix(path, ".pacnew")
		fmt.Fprintf(&buf, "diff for %s:\n", path)
		if _, err := os.Stat(original); errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(&buf, "  (current file does not exist)")
			continue
		}
		cmd := exec.CommandContext(ctx, "diff", "-u", original, path)
		out, err := cmd.CombinedOutput()
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				fmt.Fprintf(&buf, "  (could not diff files: %v)\n", err)
				continue
			}
		}
		lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
		if len(lines) == 1 && lines[0] == "" {
			fmt.Fprintln(&buf, "  (no differences found)")
			continue
		}
		if len(lines) > maxLines {
			lines = lines[:maxLines]
		}
		for _, line := range lines {
			fmt.Fprintf(&buf, "  %s\n", line)
		}
	}
	return strings.TrimRight(buf.String(), "\n")
}

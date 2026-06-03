// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package packages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.starlark.net/starlark"
)

// Module returns the Starlark pkg module.
func Module() boot.Module { return &module{} }

type module struct {
	mu      sync.Mutex
	manager string
	pkgMu   sync.Mutex
}

func (*module) Name() string { return "pkg" }

func (m *module) Members(rt *boot.Runtime) starlark.StringDict {
	impl := &impl{rt: rt, mod: m}
	return starlark.StringDict{
		"check_explicit_packages": starlark.NewBuiltin("pkg.check_explicit_packages", impl.checkExplicitPackages),
		"configure":               starlark.NewBuiltin("pkg.configure", impl.configure),
		"install":                 starlark.NewBuiltin("pkg.install", impl.install),
		"manager":                 starlark.NewBuiltin("pkg.manager", impl.manager),
		"update":                  starlark.NewBuiltin("pkg.update", impl.update),
	}
}

type impl struct {
	rt  *boot.Runtime
	mod *module
}

func (m *impl) configure(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var manager string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "manager", &manager); err != nil {
		return nil, err
	}
	if _, err := packageManagerByName(m.rt, manager); err != nil {
		return nil, err
	}
	m.mod.mu.Lock()
	m.mod.manager = manager
	m.mod.mu.Unlock()
	return starlark.None, nil
}

func (m *impl) manager(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 || len(kwargs) > 0 {
		return nil, fmt.Errorf("%s: unexpected arguments", b.Name())
	}
	pm, err := m.resolveManager()
	if err != nil {
		return nil, err
	}
	return starlark.String(pm.name), nil
}

func (m *impl) install(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := boot.RequireTask(thread, b); err != nil {
		return nil, err
	}

	var packages *starlark.List
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "packages", &packages); err != nil {
		return nil, err
	}
	names, err := boot.StringList("packages", packages)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	if len(names) == 0 {
		return starlark.None, nil
	}
	slices.Sort(names)
	names = slices.Compact(names)

	pm, err := m.resolveManager()
	if err != nil {
		return nil, err
	}

	boot.AddAction(thread, boot.Action{
		Summary:      fmt.Sprintf("install packages with %s: %s", pm.name, strings.Join(names, ", ")),
		RequiresSudo: pm.requiresSudo,
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			m.mod.pkgMu.Lock()
			defer m.mod.pkgMu.Unlock()

			missing, err := pm.missing(ctx, names)
			if err != nil {
				return "", err
			}
			if len(missing) == 0 {
				return boot.ResultSkip, nil
			}
			if dryRun {
				return boot.ResultChange, nil
			}
			return boot.ResultChange, pm.install(ctx, missing)
		},
	})
	return starlark.None, nil
}

func (m *impl) update(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := boot.RequireTask(thread, b); err != nil {
		return nil, err
	}

	if err := starlark.UnpackArgs(b.Name(), args, kwargs); err != nil {
		return nil, err
	}

	pm, err := m.resolveManager()
	if err != nil {
		return nil, err
	}

	boot.AddAction(thread, boot.Action{
		Summary:      fmt.Sprintf("update system with %s", pm.name),
		RequiresSudo: pm.requiresSudo,
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			m.mod.pkgMu.Lock()
			defer m.mod.pkgMu.Unlock()

			updates, err := pm.updates(ctx)
			if err != nil {
				return "", err
			}
			if len(updates) == 0 {
				return boot.ResultSkip, nil
			}
			if dryRun {
				return boot.ResultChange, nil
			}
			return boot.ResultChange, pm.update(ctx)
		},
	})
	return starlark.None, nil
}

func (m *impl) checkExplicitPackages(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := boot.RequireTask(thread, b); err != nil {
		return nil, err
	}
	var packages *starlark.List
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "packages", &packages); err != nil {
		return nil, err
	}
	defined, err := boot.StringList("packages", packages)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	slices.Sort(defined)
	defined = slices.Compact(defined)
	pm, err := m.resolveManager()
	if err != nil {
		return nil, err
	}
	boot.AddAction(thread, boot.Action{
		Summary: fmt.Sprintf("check explicit packages with %s", pm.name),
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			installed, err := pm.explicit(ctx)
			if err != nil {
				return "", err
			}
			known := make(map[string]bool)
			for _, pkg := range defined {
				known[pkg] = true
			}
			var extra []string
			for _, pkg := range installed {
				if !known[pkg] {
					extra = append(extra, pkg)
				}
			}
			if len(extra) == 0 {
				return boot.ResultSkip, nil
			}
			slices.Sort(extra)
			fmt.Fprintf(boot.Output(m.rt), "explicit packages missing from recipe:\n%s\n", boot.BulletList(extra))
			return boot.ResultWarn, nil
		},
	})
	return starlark.None, nil
}

func (m *impl) resolveManager() (packageManager, error) {
	m.mod.mu.Lock()
	defer m.mod.mu.Unlock()

	if m.mod.manager != "" {
		return packageManagerByName(m.rt, m.mod.manager)
	}

	manager := m.rt.EnvValue("BOOT_PACKAGE_MANAGER")
	if manager == "" {
		if _, err := exec.LookPath("pacman"); err == nil {
			manager = "pacman"
		} else if _, err := exec.LookPath("apt"); err == nil {
			manager = "apt"
		} else if _, err := exec.LookPath("pkg"); err == nil {
			manager = "apt"
		}
	}

	if manager != "" {
		m.mod.manager = manager
		return packageManagerByName(m.rt, manager)
	}

	return packageManager{}, errors.New("no supported package manager found")
}

type packageManager struct {
	name         string
	requiresSudo bool
	missing      func(context.Context, []string) ([]string, error)
	explicit     func(context.Context) ([]string, error)
	installArgv  func([]string) []string
	updates      func(context.Context) ([]string, error)
	update       func(context.Context) error
}

func (pm packageManager) install(ctx context.Context, packages []string) error {
	argv := pm.installArgv(packages)
	return boot.RunCommand(ctx, "", argv...)
}

func sudoArgs(rt *boot.Runtime, args ...string) []string {
	if rt.NeedsSudo() {
		return append([]string{"sudo"}, args...)
	}
	return args
}

func pacmanUpdates(ctx context.Context, rt *boot.Runtime) ([]string, error) {
	// Follow checkupdates' safe-update pattern: synchronize a separate pacman
	// database, then compare the installed local database against that copy. This
	// avoids running pacman -Sy against the system database without immediately
	// upgrading, which can leave the host in a partial-upgrade-prone state.
	dbpath, err := pacmanCheckDBPath(rt)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(dbpath, 0o755); err != nil {
		return nil, err
	}
	local := filepath.Join(dbpath, "local")
	// pacman -Qu needs the installed package database. Like checkupdates, keep
	// using the system "local" database through a symlink while storing synced
	// repository databases in boot's cache directory.
	if _, err := os.Lstat(local); errors.Is(err, os.ErrNotExist) {
		pacmanDBPath := pacmanSystemDBPath(ctx)
		if err := os.Symlink(filepath.Join(pacmanDBPath, "local"), local); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	// Sync only the cached database. fakeroot lets pacman create root-owned
	// database metadata in the cache without requiring actual privileges.
	cmd := exec.CommandContext(ctx, "fakeroot", "--", "pacman", "-Sy", "--disable-sandbox-filesystem", "--dbpath", dbpath, "--logfile", os.DevNull)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, boot.CommandError(cmd.Args, out, err)
	}

	// Query pending upgrades against the cached sync database. pacman exits 1
	// when no upgrades are available, which is a successful no-op for boot.
	cmd = exec.CommandContext(ctx, "pacman", "-Qu", "--dbpath", dbpath)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return nil, err
		}
	}

	var updates []string
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		// checkupdates suppresses bracketed pacman notes such as ignored packages;
		// they are not actionable updates for pkg.update.
		if line == "" || strings.Contains(line, "[") && strings.Contains(line, "]") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			updates = append(updates, fields[0])
		}
	}
	return updates, nil
}

func pacmanCheckDBPath(rt *boot.Runtime) (string, error) {
	cacheHome := ""
	if rt != nil {
		cacheHome = rt.EnvValue("XDG_CACHE_HOME")
	}
	if cacheHome == "" {
		home := ""
		if rt != nil {
			home = rt.Home
		}
		if home == "" && rt != nil {
			home = rt.EnvValue("HOME")
		}
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return "", err
			}
		}
		cacheHome = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheHome, "boot", "pacman"), nil
}

func pacmanSystemDBPath(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "pacman-conf", "DBPath")
	out, err := cmd.Output()
	if err != nil {
		return "/var/lib/pacman"
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "/var/lib/pacman"
	}
	return path
}

func packageManagerByName(rt *boot.Runtime, name string) (packageManager, error) {
	switch name {
	case "apt":
		return packageManager{
			name:         "apt",
			requiresSudo: rt.NeedsSudo(),
			missing: func(ctx context.Context, packages []string) ([]string, error) {
				// dpkg-query -W -f='${Package} ${Status}\n' pkg1 pkg2 ...
				args := append([]string{"-W", "-f=${Package} ${Status}\n"}, packages...)
				cmd := exec.CommandContext(ctx, "dpkg-query", args...)
				out, _ := cmd.CombinedOutput() // ignore error, as it returns 1 if any package is missing.

				found := make(map[string]bool)
				lines := strings.SplitSeq(string(out), "\n")
				for line := range lines {
					if strings.Contains(line, "install ok installed") {
						fields := strings.Fields(line)
						if len(fields) > 0 {
							found[fields[0]] = true
						}
					}
				}

				var missing []string
				for _, pkg := range packages {
					if !found[pkg] {
						missing = append(missing, pkg)
					}
				}
				return missing, nil
			},
			explicit: func(ctx context.Context) ([]string, error) {
				cmd := exec.CommandContext(ctx, "apt-mark", "showmanual")
				out, err := cmd.Output()
				if err != nil {
					return nil, err
				}
				return strings.Fields(string(out)), nil
			},
			installArgv: func(packages []string) []string {
				return sudoArgs(rt, append([]string{"apt", "install", "-y"}, packages...)...)
			},
			updates: func(context.Context) ([]string, error) {
				return []string{"system"}, nil
			},
			update: func(ctx context.Context) error {
				var cmdLine string
				if rt.NeedsSudo() {
					cmdLine = "sudo apt update && sudo apt upgrade -y"
				} else {
					cmdLine = "apt update && apt upgrade -y"
				}
				return boot.RunCommand(ctx, "", "sh", "-c", cmdLine)
			},
		}, nil
	case "pacman":
		return packageManager{
			name:         "pacman",
			requiresSudo: rt.NeedsSudo(),
			missing: func(ctx context.Context, packages []string) ([]string, error) {
				// pacman -T lists missing packages.
				args := append([]string{"-T"}, packages...)
				cmd := exec.CommandContext(ctx, "pacman", args...)
				out, err := cmd.Output()
				if err != nil {
					// pacman -T exits non-zero when dependencies are missing and
					// prints the missing dependency names on stdout.
					var exitErr *exec.ExitError
					if errors.As(err, &exitErr) && len(strings.TrimSpace(string(out))) > 0 {
						return strings.Fields(string(out)), nil
					}
					return nil, err
				}
				return nil, nil
			},
			explicit: func(ctx context.Context) ([]string, error) {
				cmd := exec.CommandContext(ctx, "pacman", "-Qqen")
				out, err := cmd.Output()
				if err != nil {
					return nil, err
				}
				return strings.Fields(string(out)), nil
			},
			installArgv: func(packages []string) []string {
				return sudoArgs(rt, append([]string{"pacman", "-S", "--noconfirm", "--needed"}, packages...)...)
			},
			updates: func(ctx context.Context) ([]string, error) {
				return pacmanUpdates(ctx, rt)
			},
			update: func(ctx context.Context) error {
				args := sudoArgs(rt, "pacman", "-Syu", "--noconfirm")
				return boot.RunCommand(ctx, "", args...)
			},
		}, nil
	default:
		return packageManager{}, fmt.Errorf("unsupported package manager %q", name)
	}
}

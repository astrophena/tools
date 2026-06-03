// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package systemd

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.starlark.net/starlark"
)

// Module returns the Starlark systemd module.
func Module() boot.Module { return module{} }

type module struct{}

func (module) Name() string { return "systemd" }

func (module) Members(rt *boot.Runtime) starlark.StringDict {
	m := &impl{rt: rt}
	return starlark.StringDict{
		"system_unit": starlark.NewBuiltin("systemd.system_unit", m.systemUnit),
		"user_unit":   starlark.NewBuiltin("systemd.user_unit", m.userUnit),
	}
}

type impl struct {
	rt *boot.Runtime
}

func (m *impl) userUnit(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return m.unit(thread, b, true, args, kwargs)
}

func (m *impl) systemUnit(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return m.unit(thread, b, false, args, kwargs)
}

func (m *impl) unit(thread *starlark.Thread, b *starlark.Builtin, user bool, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := boot.RequireTask(thread, b); err != nil {
		return nil, err
	}
	var (
		name         string
		enabled      bool
		started      bool
		daemonReload bool
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"name", &name,
		"enabled?", &enabled,
		"started?", &started,
		"daemon_reload?", &daemonReload,
	); err != nil {
		return nil, err
	}
	if name == "" {
		return nil, fmt.Errorf("%s: name cannot be empty", b.Name())
	}

	boot.AddAction(thread, boot.Action{
		Summary:      "systemd " + scopeName(user) + " unit " + name,
		RequiresSudo: !user && m.rt.NeedsSudo(),
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			needsChange := false
			needsReload := false
			needsEnable := false
			needsStart := false
			if daemonReload {
				var err error
				needsReload, err = unitNeedsReload(ctx, m.rt, user, name)
				if err != nil {
					return "", err
				}
				needsChange = needsChange || needsReload
			}
			if enabled {
				ok, err := systemctlQuiet(ctx, m.rt, user, "is-enabled", name)
				if err != nil {
					return "", err
				}
				needsEnable = !ok
				needsChange = needsChange || needsEnable
			}
			if started {
				ok, err := systemctlQuiet(ctx, m.rt, user, "is-active", name)
				if err != nil {
					return "", err
				}
				needsStart = !ok
				needsChange = needsChange || needsStart
			}
			if !needsChange {
				return boot.ResultSkip, nil
			}
			if dryRun {
				return boot.ResultChange, nil
			}
			if needsReload {
				if err := runSystemctl(ctx, m.rt, user, "daemon-reload"); err != nil {
					return "", err
				}
			}
			if needsEnable || needsStart {
				args := []string{"enable"}
				if needsStart {
					args = append(args, "--now")
				}
				args = append(args, name)
				if err := runSystemctl(ctx, m.rt, user, args...); err != nil {
					return "", err
				}
			}
			return boot.ResultChange, nil
		},
	})
	return starlark.None, nil
}

func unitNeedsReload(ctx context.Context, rt *boot.Runtime, user bool, name string) (bool, error) {
	cmd := systemctlCommand(ctx, rt, user, "show", name, "--property=NeedDaemonReload", "--value")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, boot.CommandError(cmd.Args, out, err)
	}
	switch strings.TrimSpace(string(out)) {
	case "yes":
		return true, nil
	case "no", "":
		return false, nil
	default:
		return true, nil
	}
}

func systemctlQuiet(ctx context.Context, rt *boot.Runtime, user bool, args ...string) (bool, error) {
	cmd := systemctlCommand(ctx, rt, user, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}
	if !isSystemctlInactiveStatus(out) {
		return false, boot.CommandError(cmd.Args, out, err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false, err
	}
	return false, nil
}

func isSystemctlInactiveStatus(out []byte) bool {
	status := strings.TrimSpace(string(out))
	switch status {
	case "disabled", "inactive", "failed", "not-found", "unknown":
		return true
	}
	return strings.Contains(string(out), "could not be found")
}

func runSystemctl(ctx context.Context, rt *boot.Runtime, user bool, args ...string) error {
	cmd := systemctlCommand(ctx, rt, user, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	return boot.CommandError(cmd.Args, out, err)
}

func systemctlCommand(ctx context.Context, rt *boot.Runtime, user bool, args ...string) *exec.Cmd {
	argv := []string{"systemctl"}
	if user {
		argv = append(argv, "--user")
	} else if rt.NeedsSudo() {
		argv = []string{"sudo", "systemctl"}
	}
	argv = append(argv, args...)
	return exec.CommandContext(ctx, argv[0], argv[1:]...)
}

func scopeName(user bool) string {
	if user {
		return "user"
	}
	return "system"
}

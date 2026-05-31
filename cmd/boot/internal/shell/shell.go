// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.starlark.net/starlark"
)

// Module returns the Starlark shell module.
func Module() boot.Module { return module{} }

type module struct{}

func (module) Name() string { return "shell" }

func (module) Members(rt *boot.Runtime) starlark.StringDict {
	m := &impl{rt: rt}
	return starlark.StringDict{
		"run":    starlark.NewBuiltin("shell.run", m.run),
		"output": starlark.NewBuiltin("shell.output", m.output),
	}
}

type impl struct {
	rt *boot.Runtime
}

func (m *impl) run(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var (
		command string
		creates string
		onlyIf  string
		cwd     string
		sudo    bool
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"command", &command,
		"creates?", &creates,
		"only_if?", &onlyIf,
		"cwd?", &cwd,
		"sudo?", &sudo,
	); err != nil {
		return nil, err
	}

	summary := "run: " + command
	if creates != "" {
		summary += " (creates " + creates + ")"
	}
	if onlyIf != "" {
		summary += " (only_if " + onlyIf + ")"
	}
	if cwd != "" {
		summary += " (cwd " + cwd + ")"
	}
	if sudo {
		summary += " (sudo)"
	}

	boot.AddAction(thread, boot.Action{
		Summary: summary,
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			if creates != "" {
				abs := m.rt.ResolveTarget(creates)
				if _, err := os.Stat(abs); err == nil {
					return boot.ResultSkip, nil
				}
			}

			if onlyIf != "" {
				if dryRun && sudo && m.rt.NeedsSudo() {
					return boot.ResultChange, nil
				}
				var cmd *exec.Cmd
				if sudo && m.rt.NeedsSudo() {
					cmd = exec.CommandContext(ctx, "sudo", "sh", "-c", onlyIf)
				} else {
					cmd = exec.CommandContext(ctx, "sh", "-c", onlyIf)
				}
				if cwd != "" {
					cmd.Dir = m.rt.ResolveTarget(cwd)
				}
				if err := cmd.Run(); err != nil {
					return boot.ResultSkip, nil
				}
			}

			if dryRun {
				return boot.ResultChange, nil
			}

			var cmd *exec.Cmd
			if sudo && m.rt.NeedsSudo() {
				cmd = exec.CommandContext(ctx, "sudo", "sh", "-c", command)
			} else {
				cmd = exec.CommandContext(ctx, "sh", "-c", command)
			}
			if cwd != "" {
				cmd.Dir = m.rt.ResolveTarget(cwd)
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				msg := strings.TrimSpace(string(out))
				if msg == "" {
					return "", err
				}
				return "", fmt.Errorf("%w:\n%s", err, msg)
			}

			return boot.ResultChange, nil
		},
	})

	return starlark.None, nil
}

func (m *impl) output(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		command string
		cwd     string
		sudo    bool
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"command", &command,
		"cwd?", &cwd,
		"sudo?", &sudo,
	); err != nil {
		return nil, err
	}

	var cmd *exec.Cmd
	if sudo && m.rt.NeedsSudo() {
		cmd = exec.Command("sudo", "sh", "-c", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	if cwd != "" {
		cmd.Dir = m.rt.ResolveTarget(cwd)
	}

	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return nil, fmt.Errorf("%s failed: %w", command, err)
		}
		return nil, fmt.Errorf("%s failed: %w:\n%s", command, err, msg)
	}
	return starlark.String(out), nil
}

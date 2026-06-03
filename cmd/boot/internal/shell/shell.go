// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package shell

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"

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
	if err := boot.RequireTask(thread, b); err != nil {
		return nil, err
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
		Summary:      summary,
		RequiresSudo: sudo && m.rt.NeedsSudo(),
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			if creates != "" {
				abs := m.rt.ResolveTarget(creates)
				if _, err := os.Stat(abs); err == nil {
					return boot.ResultSkip, nil
				} else if !errors.Is(err, fs.ErrNotExist) {
					return "", err
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
			if err := boot.RunCmd(cmd); err != nil {
				return "", err
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

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		combined := append(append([]byte{}, out...), stderr.Bytes()...)
		return nil, boot.CommandError(cmd.Args, combined, err)
	}
	return starlark.String(out), nil
}

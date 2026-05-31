// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package flatpak

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.starlark.net/starlark"
)

// Module returns the Starlark flatpak module.
func Module() boot.Module { return module{} }

type module struct{}

func (module) Name() string { return "flatpak" }

func (module) Members(rt *boot.Runtime) starlark.StringDict {
	m := &impl{rt: rt}
	return starlark.StringDict{
		"update": starlark.NewBuiltin("flatpak.update", m.update),
	}
}

type impl struct {
	rt *boot.Runtime
}

func (m *impl) update(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}
	if err := starlark.UnpackArgs(b.Name(), args, kwargs); err != nil {
		return nil, err
	}

	boot.AddAction(thread, boot.Action{
		Summary: "update flatpak applications",
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			if _, err := exec.LookPath("flatpak"); err != nil {
				return boot.ResultSkip, nil
			}
			cmd := exec.CommandContext(ctx, "flatpak", "remote-ls", "--updates")
			out, err := cmd.Output()
			if err != nil {
				return "", err
			}
			if len(bytes.TrimSpace(out)) == 0 {
				return boot.ResultSkip, nil
			}
			if dryRun {
				return boot.ResultChange, nil
			}
			cmd = exec.CommandContext(ctx, "flatpak", "update", "-y")
			out, err = cmd.CombinedOutput()
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

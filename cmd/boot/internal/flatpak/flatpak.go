// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package flatpak

import (
	"bytes"
	"context"
	"os/exec"

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
	if err := boot.RequireTask(thread, b); err != nil {
		return nil, err
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
			out, err := boot.CommandOutput(ctx, "", "flatpak", "remote-ls", "--updates")
			if err != nil {
				return "", err
			}
			if len(bytes.TrimSpace(out)) == 0 {
				return boot.ResultSkip, nil
			}
			if dryRun {
				return boot.ResultChange, nil
			}
			if err := boot.RunCommand(ctx, "", "flatpak", "update", "-y"); err != nil {
				return "", err
			}
			return boot.ResultChange, nil
		},
	})
	return starlark.None, nil
}

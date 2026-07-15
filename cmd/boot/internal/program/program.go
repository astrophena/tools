// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package program

import (
	"context"
	"fmt"
	"slices"
	"strings"

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.starlark.net/starlark"
)

const checkFlag = "-check"

// Module returns the Starlark program module.
func Module() boot.Module { return module{} }

type module struct{}

func (module) Name() string { return "program" }

func (module) Members(*boot.Runtime) starlark.StringDict {
	m := new(impl)
	return starlark.StringDict{
		"update": starlark.NewBuiltin("program.update", m.update),
	}
}

type impl struct{}

func (m *impl) update(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := boot.RequireTask(thread, b); err != nil {
		return nil, err
	}

	var list *starlark.List
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "argv", &list); err != nil {
		return nil, err
	}
	argv, err := unpackArgv(list)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}

	boot.AddAction(thread, boot.Action{
		Summary: "update program: " + strings.Join(argv, " "),
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			checkArgv := append(slices.Clone(argv), checkFlag)
			out, err := boot.CommandOutput(ctx, "", checkArgv...)
			if err != nil {
				return "", err
			}
			switch strings.TrimSpace(string(out)) {
			case "false":
				return boot.ResultSkip, nil
			case "true":
				if dryRun {
					return boot.ResultChange, nil
				}
				if err := boot.RunCommand(ctx, "", argv...); err != nil {
					return "", err
				}
				return boot.ResultChange, nil
			default:
				return "", fmt.Errorf("%s returned invalid update check result %q", strings.Join(checkArgv, " "), strings.TrimSpace(string(out)))
			}
		},
	})
	return starlark.None, nil
}

func unpackArgv(list *starlark.List) ([]string, error) {
	if list == nil || list.Len() == 0 {
		return nil, fmt.Errorf("argv cannot be empty")
	}
	argv := make([]string, 0, list.Len())
	for i := range list.Len() {
		arg, ok := starlark.AsString(list.Index(i))
		if !ok {
			return nil, fmt.Errorf("argv[%d] is not a string", i)
		}
		if arg == "" {
			return nil, fmt.Errorf("argv[%d] cannot be empty", i)
		}
		if arg == checkFlag || strings.HasPrefix(arg, checkFlag+"=") {
			return nil, fmt.Errorf("argv[%d] uses reserved flag %s", i, checkFlag)
		}
		argv = append(argv, arg)
	}
	return argv, nil
}

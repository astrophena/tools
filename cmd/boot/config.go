// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.astrophena.name/base/cli"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

type options struct {
	workspace   string
	entry       string
	concurrency int
	failFast    bool
	verbose     bool
	json        bool
}

func loadConfig(ctx context.Context, env *cli.Env, cfg *options) error {
	path, err := configPath(env)
	if err != nil {
		return err
	}
	src, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	thread := &starlark.Thread{
		Name: "boot:config",
	}
	stop := context.AfterFunc(ctx, func() { thread.Cancel(ctx.Err().Error()) })
	defer stop()
	predeclared := starlark.StringDict{
		"boot": &starlarkstruct.Module{
			Name: "boot",
			Members: starlark.StringDict{
				"configure": starlark.NewBuiltin("boot.configure", cfg.configure),
			},
		},
	}
	if _, err := starlark.ExecFileOptions(&syntax.FileOptions{}, thread, path, src, predeclared); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func configPath(env *cli.Env) (string, error) {
	if xdg := env.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(expandHome(env, xdg), "boot", "config.star"), nil
	}
	home := env.Getenv("HOME")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(home, ".config", "boot", "config.star"), nil
}

func (c *options) configure(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 {
		return nil, fmt.Errorf("%s: unexpected positional arguments", b.Name())
	}
	var concurrency starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"workspace?", &c.workspace,
		"entry?", &c.entry,
		"concurrency?", &concurrency,
		"fail_fast?", &c.failFast,
		"verbose?", &c.verbose,
		"json?", &c.json,
	); err != nil {
		return nil, err
	}
	if concurrency != nil {
		n, err := starlark.AsInt32(concurrency)
		if err != nil {
			return nil, fmt.Errorf("%s: concurrency must be an integer", b.Name())
		}
		if n < 1 {
			return nil, fmt.Errorf("%s: concurrency must be at least 1", b.Name())
		}
		c.concurrency = n
	}
	return starlark.None, nil
}

func expandHome(env *cli.Env, path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home := env.Getenv("HOME")
		if home == "" {
			if userHome, err := os.UserHomeDir(); err == nil {
				home = userHome
			}
		}
		if home != "" {
			return filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/")))
		}
	}
	return path
}

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

type configOptions struct {
	workspace   configValue[string]
	entry       configValue[string]
	concurrency configValue[int]
	failFast    configValue[bool]
	verbose     configValue[bool]
	json        configValue[bool]
}

type configValue[T any] struct {
	value T
	set   bool
}

func (v *configValue[T]) setValue(value T) {
	v.value = value
	v.set = true
}

func loadConfig(ctx context.Context, env *cli.Env) (configOptions, error) {
	path, err := configPath(env)
	if err != nil {
		return configOptions{}, err
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return configOptions{}, nil
	} else if err != nil {
		return configOptions{}, err
	}

	cfg := configOptions{}
	thread := &starlark.Thread{Name: "boot:config"}
	predeclared := starlark.StringDict{
		"boot": &starlarkstruct.Module{
			Name: "boot",
			Members: starlark.StringDict{
				"configure": starlark.NewBuiltin("boot.configure", cfg.configure),
			},
		},
	}
	if _, err := starlark.ExecFileOptions(&syntax.FileOptions{}, thread, path, nil, predeclared); err != nil {
		return configOptions{}, fmt.Errorf("%s: %w", path, err)
	}
	select {
	case <-ctx.Done():
		return configOptions{}, ctx.Err()
	default:
	}
	return cfg, nil
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

func (c *configOptions) configure(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 {
		return nil, fmt.Errorf("%s: unexpected positional arguments", b.Name())
	}
	for _, kw := range kwargs {
		name := string(kw[0].(starlark.String))
		value := kw[1]
		switch name {
		case "workspace":
			s, ok := starlark.AsString(value)
			if !ok {
				return nil, fmt.Errorf("%s: workspace must be a string", b.Name())
			}
			c.workspace.setValue(s)
		case "entry":
			s, ok := starlark.AsString(value)
			if !ok {
				return nil, fmt.Errorf("%s: entry must be a string", b.Name())
			}
			c.entry.setValue(s)
		case "concurrency":
			n, err := starlark.AsInt32(value)
			if err != nil {
				return nil, fmt.Errorf("%s: concurrency must be an integer", b.Name())
			}
			if n < 1 {
				return nil, fmt.Errorf("%s: concurrency must be at least 1", b.Name())
			}
			c.concurrency.setValue(n)
		case "fail_fast":
			v, ok := value.(starlark.Bool)
			if !ok {
				return nil, fmt.Errorf("%s: fail_fast must be a bool", b.Name())
			}
			c.failFast.setValue(bool(v))
		case "verbose":
			v, ok := value.(starlark.Bool)
			if !ok {
				return nil, fmt.Errorf("%s: verbose must be a bool", b.Name())
			}
			c.verbose.setValue(bool(v))
		case "json":
			v, ok := value.(starlark.Bool)
			if !ok {
				return nil, fmt.Errorf("%s: json must be a bool", b.Name())
			}
			c.json.setValue(bool(v))
		default:
			return nil, fmt.Errorf("%s: unknown option %q", b.Name(), name)
		}
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

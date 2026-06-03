// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package env

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.starlark.net/starlark"
)

// Module returns the Starlark env module.
func Module() boot.Module { return module{} }

type module struct{}

func (module) Name() string { return "env" }

func (module) Members(rt *boot.Runtime) starlark.StringDict {
	m := &impl{rt: rt}
	return starlark.StringDict{
		"command_exists": starlark.NewBuiltin("env.command_exists", m.commandExists),
		"get":            starlark.NewBuiltin("env.get", m.get),
		"hostname":       starlark.NewBuiltin("env.hostname", m.hostname),
		"load_dir":       starlark.NewBuiltin("env.load_dir", m.loadDir),
	}
}

func (m *impl) commandExists(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name", &name); err != nil {
		return nil, err
	}
	_, err := exec.LookPath(name)
	return starlark.Bool(err == nil), nil
}

type impl struct {
	rt *boot.Runtime
}

func (m *impl) get(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		key        string
		defaultVal string
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"key", &key,
		"default?", &defaultVal,
	); err != nil {
		return nil, err
	}

	val := m.rt.Getenv(key)
	if val == "" {
		val = os.Getenv(key)
	}
	if val == "" {
		val = defaultVal
	}
	return starlark.String(val), nil
}

func (m *impl) hostname(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(b.Name(), args, kwargs); err != nil {
		return nil, err
	}

	name, err := m.rt.Hostname()
	if err != nil {
		return nil, err
	}
	return starlark.String(name), nil
}

func (m *impl) loadDir(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "path", &path); err != nil {
		return nil, err
	}
	dir := m.rt.ResolveSource(path)
	matches, err := filepath.Glob(filepath.Join(dir, "*.conf"))
	if err != nil {
		return nil, err
	}
	for _, match := range matches {
		if err := loadFile(match); err != nil {
			return nil, fmt.Errorf("%s: %w", match, err)
		}
	}
	return starlark.None, nil
}

func loadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		value = os.Expand(value, func(name string) string {
			return os.Getenv(name)
		})
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

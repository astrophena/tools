// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.starlark.net/starlark"
)

// Module contributes a Starlark module to a boot runtime.
type Module interface {
	// Name is the Starlark global name for this module.
	Name() string
	// Members returns the module members for the given runtime.
	Members(*Runtime) starlark.StringDict
}

// Runtime is shared state used by Starlark modules.
//
// Root is the recipe root, not necessarily /. Relative targets resolve through
// Root so tests and dry local recipes can run in temporary directories. Home is
// kept separate from process HOME because boot often manages another filesystem
// tree while still inheriting the caller's environment for child commands.
type Runtime struct {
	Root        string
	Home        string
	Getenv      func(string) string
	Env         map[string]string
	Stdin       io.Reader
	Stdout      io.Writer
	Interactive bool
	Color       bool
}

const taskKey = "boot:task"

// Starlark has per-thread local storage. Boot uses it to make module calls such
// as fs.file(...) know which task they should append actions to without exposing
// the Task object to recipes.

// SetTask associates a task with a Starlark thread.
func SetTask(thread *starlark.Thread, task *Task) {
	thread.SetLocal(taskKey, task)
}

// AddAction appends an idempotent action to the task associated with the thread.
func AddAction(thread *starlark.Thread, action Action) {
	task := thread.Local(taskKey).(*Task)
	task.Actions = append(task.Actions, action)
}

// InTask reports whether a Starlark builtin is currently running inside a task.
func InTask(thread *starlark.Thread) bool {
	return thread.Local(taskKey) != nil
}

// ResolveSource resolves a recipe source path.
//
// Sources are usually files from the recipe checkout, so relative paths are
// rooted at Runtime.Root. A leading // is accepted as an explicit recipe-root
// marker for readability in Starlark recipes.
func (r *Runtime) ResolveSource(path string) string {
	path = os.ExpandEnv(path)
	path = strings.TrimPrefix(path, "//")
	if strings.HasPrefix(path, "~/") {
		return r.ExpandHome(path)
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(r.Root, filepath.FromSlash(path))
}

// ResolveTarget resolves a target path on the host.
//
// Absolute paths are kept absolute. Relative paths are rooted at Runtime.Root,
// which is why tests can exercise host-mutating modules safely under t.TempDir.
func (r *Runtime) ResolveTarget(path string) string {
	path = os.ExpandEnv(path)
	if strings.HasPrefix(path, "~/") {
		return r.ExpandHome(path)
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(r.Root, filepath.FromSlash(path))
}

// ExpandHome expands a path beginning with ~/ against Runtime.Home.
func (r *Runtime) ExpandHome(path string) string {
	return filepath.Join(r.Home, filepath.FromSlash(strings.TrimPrefix(path, "~/")))
}

// EnvValue returns an environment value from runtime overrides, the runtime lookup function, or the process environment.
//
// Runtime.Env is checked first so env.load_dir can affect later module calls in
// the same recipe evaluation even when Runtime.Getenv comes from an immutable CLI
// environment object.
func (r *Runtime) EnvValue(key string) string {
	if r != nil {
		if val := r.Env[key]; val != "" {
			return val
		}
		if r.Getenv != nil {
			if val := r.Getenv(key); val != "" {
				return val
			}
		}
	}
	return os.Getenv(key)
}

// SetEnv stores an environment override and updates the process environment inherited by child commands.
func (r *Runtime) SetEnv(key, value string) error {
	if err := os.Setenv(key, value); err != nil {
		return err
	}
	if r != nil {
		if r.Env == nil {
			r.Env = make(map[string]string)
		}
		r.Env[key] = value
	}
	return nil
}

// BulletList formats items as an indented Markdown-style bullet list.
func BulletList(items []string) string {
	var buf strings.Builder
	for _, item := range items {
		fmt.Fprintf(&buf, "  - %s\n", item)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// NeedsSudo reports whether elevated privileges are required and available.
func (r *Runtime) NeedsSudo() bool {
	if os.Geteuid() == 0 {
		return false
	}
	if r.EnvValue("TERMUX_VERSION") != "" || strings.Contains(r.EnvValue("PREFIX"), "termux") {
		return false
	}
	return true
}

// Hostname returns the configured host name, honoring Termux's prefs hostname file.
func (r *Runtime) Hostname() (string, error) {
	if r != nil && r.Home != "" {
		path := r.ExpandHome("~/local/data/termux/hostname")
		if data, err := os.ReadFile(path); err == nil {
			if name := strings.TrimSpace(string(data)); name != "" {
				return name, nil
			}
		}
	}
	return os.Hostname()
}

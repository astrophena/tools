// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
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
type Runtime struct {
	Root        string
	Home        string
	Getenv      func(string) string
	Stdin       io.Reader
	Stdout      io.Writer
	Interactive bool
}

const taskKey = "boot:task"

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

// NeedsSudo reports whether elevated privileges are required and available.
func (r *Runtime) NeedsSudo() bool {
	if os.Geteuid() == 0 {
		return false
	}
	if r.Getenv("TERMUX_VERSION") != "" || strings.Contains(r.Getenv("PREFIX"), "termux") {
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

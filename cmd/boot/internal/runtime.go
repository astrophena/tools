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
)

// Runtime holds paths, environment, and I/O shared by Starlark modules. Root
// anchors relative paths; Home may differ from the process home directory.
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

// ResolveSource resolves a path from the recipe. Relative paths and paths
// beginning with // are rooted at Runtime.Root.
func (r *Runtime) ResolveSource(path string) string {
	path = os.ExpandEnv(path)
	return r.resolvePath(strings.TrimPrefix(path, "//"))
}

// ResolveTarget resolves a host path. Relative paths are rooted at Runtime.Root.
func (r *Runtime) ResolveTarget(path string) string {
	return r.resolvePath(os.ExpandEnv(path))
}

func (r *Runtime) resolvePath(path string) string {
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

// EnvValue looks up key in Runtime.Env, Runtime.Getenv, then the process
// environment.
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

// SetEnv stores an override and updates the environment inherited by commands.
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

// NeedsSudo reports whether the process needs sudo outside Termux.
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

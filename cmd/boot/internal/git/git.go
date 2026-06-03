// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.starlark.net/starlark"
)

// Module returns the Starlark git module.
func Module() boot.Module { return &module{} }

type module struct {
	mu    sync.Mutex
	repos map[string]*sync.Mutex
}

func (*module) Name() string { return "git" }

func (mod *module) Members(rt *boot.Runtime) starlark.StringDict {
	m := &impl{rt: rt, mod: mod}
	return starlark.StringDict{
		"clone": starlark.NewBuiltin("git.clone", m.clone),
		"pull":  starlark.NewBuiltin("git.pull", m.pull),
		"sync":  starlark.NewBuiltin("git.sync", m.sync),
	}
}

type impl struct {
	rt  *boot.Runtime
	mod *module
}

func (m *module) lockRepo(dst string) func() {
	if m == nil {
		return func() {}
	}
	m.mu.Lock()
	if m.repos == nil {
		m.repos = make(map[string]*sync.Mutex)
	}
	lock := m.repos[filepath.Clean(dst)]
	if lock == nil {
		lock = new(sync.Mutex)
		m.repos[filepath.Clean(dst)] = lock
	}
	m.mu.Unlock()

	lock.Lock()
	return lock.Unlock
}

func (m *impl) sync(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var (
		url      string
		dest     string
		revision string
	)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "url", &url, "dest", &dest, "revision?", &revision); err != nil {
		return nil, err
	}

	dst := m.rt.ResolveTarget(dest)
	summary := fmt.Sprintf("git sync %s to %s", url, dst)
	if revision != "" {
		summary += " at " + revision
	}

	boot.AddAction(thread, boot.Action{
		Summary:    summary,
		Concurrent: true,
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			unlock := m.mod.lockRepo(dst)
			defer unlock()

			if _, err := os.Stat(filepath.Join(dst, ".git")); errors.Is(err, fs.ErrNotExist) {
				if dryRun {
					return boot.ResultChange, nil
				}
				if err := cloneRepository(ctx, url, dst, revision); err != nil {
					return "", err
				}
				return boot.ResultChange, nil
			} else if err != nil {
				return "", err
			}

			if dirty, err := isDirty(ctx, dst); err != nil {
				return "", err
			} else if dirty {
				return boot.ResultSkip, nil
			}

			if revision != "" {
				return syncRevision(ctx, dst, revision, dryRun)
			}

			if !hasUpstream(ctx, dst) {
				return boot.ResultSkip, nil
			}

			if dryRun {
				current, upstream, err := gitRevisions(ctx, dst)
				if err != nil {
					return "", err
				}
				if current == upstream {
					return boot.ResultSkip, nil
				}
				return boot.ResultChange, nil
			}

			result, err := fastForward(ctx, dst)
			if err != nil {
				return "", err
			}
			return result, nil
		},
	})

	return starlark.None, nil
}

func (m *impl) pull(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var dest string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "dest", &dest); err != nil {
		return nil, err
	}

	dst := m.rt.ResolveTarget(dest)

	boot.AddAction(thread, boot.Action{
		Summary:    fmt.Sprintf("git pull %s", dst),
		Concurrent: true,
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			unlock := m.mod.lockRepo(dst)
			defer unlock()

			if _, err := os.Stat(filepath.Join(dst, ".git")); errors.Is(err, fs.ErrNotExist) {
				return "", fmt.Errorf("not a git repository: %s", dst)
			}

			cmd := exec.CommandContext(ctx, "git", "-C", dst, "status", "--porcelain")
			out, err := cmd.Output()
			if err != nil {
				return "", err
			}
			if len(out) > 0 {
				return boot.ResultSkip, nil
			}

			if !hasUpstream(ctx, dst) {
				return boot.ResultSkip, nil
			}

			if dryRun {
				current, upstream, err := gitRevisions(ctx, dst)
				if err != nil {
					return "", err
				}
				if current == upstream {
					return boot.ResultSkip, nil
				}
				return boot.ResultChange, nil
			}

			result, err := fastForward(ctx, dst)
			if err != nil {
				return "", err
			}
			return result, nil
		},
	})

	return starlark.None, nil
}

func (m *impl) clone(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var (
		url      string
		dest     string
		revision string
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"url", &url,
		"dest", &dest,
		"revision?", &revision,
	); err != nil {
		return nil, err
	}

	dst := m.rt.ResolveTarget(dest)
	summary := fmt.Sprintf("git clone %s to %s", url, dst)
	if revision != "" {
		summary += " at " + revision
	}

	boot.AddAction(thread, boot.Action{
		Summary:    summary,
		Concurrent: true,
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			unlock := m.mod.lockRepo(dst)
			defer unlock()

			if _, err := os.Stat(filepath.Join(dst, ".git")); err == nil {
				if revision == "" {
					return boot.ResultSkip, nil
				}
				if dirty, err := isDirty(ctx, dst); err != nil {
					return "", err
				} else if dirty {
					return boot.ResultSkip, nil
				}
				return syncRevision(ctx, dst, revision, dryRun)
			} else if !errors.Is(err, fs.ErrNotExist) {
				return "", err
			}

			if dryRun {
				return boot.ResultChange, nil
			}

			if err := cloneRepository(ctx, url, dst, revision); err != nil {
				return "", err
			}

			return boot.ResultChange, nil
		},
	})

	return starlark.None, nil
}

func run(cmd *exec.Cmd) error {
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return err
	}
	return fmt.Errorf("%w:\n%s", err, msg)
}

func hasUpstream(ctx context.Context, dst string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", dst, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	return cmd.Run() == nil
}

func fastForward(ctx context.Context, dst string) (boot.Result, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dst, "fetch", "--prune", "origin")
	if err := run(cmd); err != nil {
		return "", err
	}

	current, upstream, err := gitRevisions(ctx, dst)
	if err != nil {
		return "", err
	}
	if current == upstream {
		return boot.ResultSkip, nil
	}

	cmd = exec.CommandContext(ctx, "git", "-C", dst, "pull", "--ff-only")
	if err := run(cmd); err != nil {
		return "", err
	}

	return boot.ResultChange, nil
}

func gitRevisions(ctx context.Context, dst string) (string, string, error) {
	current, err := gitRevision(ctx, dst, "HEAD")
	if err != nil {
		return "", "", err
	}
	upstream, err := gitRevision(ctx, dst, "@{u}")
	if err != nil {
		return "", "", err
	}
	return current, upstream, nil
}

func gitRevision(ctx context.Context, dst, rev string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dst, "rev-parse", "--verify", rev)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func cloneRepository(ctx context.Context, url, dst, revision string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "clone", url, dst)
	if err := run(cmd); err != nil {
		return err
	}
	if revision == "" {
		return nil
	}
	return checkoutRevision(ctx, dst, revision)
}

func isDirty(ctx context.Context, dst string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dst, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(out) > 0, nil
}

func syncRevision(ctx context.Context, dst, revision string, dryRun bool) (boot.Result, error) {
	current, err := gitRevision(ctx, dst, "HEAD")
	if err != nil {
		return "", err
	}
	resolved, err := resolveRevision(ctx, dst, revision)
	if err == nil && current == resolved {
		return boot.ResultSkip, nil
	}
	if dryRun {
		return boot.ResultChange, nil
	}
	if err := fetchRepository(ctx, dst); err != nil {
		return "", err
	}
	resolved, err = resolveRevision(ctx, dst, revision)
	if err != nil {
		return "", err
	}
	if current == resolved {
		return boot.ResultSkip, nil
	}
	if err := checkoutRevision(ctx, dst, resolved); err != nil {
		return "", err
	}
	return boot.ResultChange, nil
}

func fetchRepository(ctx context.Context, dst string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", dst, "fetch", "--tags", "--prune", "origin")
	return run(cmd)
}

func checkoutRevision(ctx context.Context, dst, revision string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", dst, "checkout", "--detach", revision)
	return run(cmd)
}

func resolveRevision(ctx context.Context, dst, revision string) (string, error) {
	return gitRevision(ctx, dst, revision+"^{commit}")
}

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

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.starlark.net/starlark"
)

// Module returns the Starlark git module.
func Module() boot.Module { return module{} }

type module struct{}

func (module) Name() string { return "git" }

func (module) Members(rt *boot.Runtime) starlark.StringDict {
	m := &impl{rt: rt}
	return starlark.StringDict{
		"clone": starlark.NewBuiltin("git.clone", m.clone),
		"pull":  starlark.NewBuiltin("git.pull", m.pull),
		"sync":  starlark.NewBuiltin("git.sync", m.sync),
	}
}

type impl struct {
	rt *boot.Runtime
}

func (m *impl) sync(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var (
		url  string
		dest string
	)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "url", &url, "dest", &dest); err != nil {
		return nil, err
	}

	dst := m.rt.ResolveTarget(dest)

	boot.AddAction(thread, boot.Action{
		Summary: fmt.Sprintf("git sync %s to %s", url, dst),
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			if _, err := os.Stat(filepath.Join(dst, ".git")); errors.Is(err, fs.ErrNotExist) {
				if dryRun {
					return boot.ResultChange, nil
				}
				if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
					return "", err
				}
				cmd := exec.CommandContext(ctx, "git", "clone", url, dst)
				if err := run(cmd); err != nil {
					return "", err
				}
				return boot.ResultChange, nil
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
		Summary: fmt.Sprintf("git pull %s", dst),
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
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
		url  string
		dest string
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"url", &url,
		"dest", &dest,
	); err != nil {
		return nil, err
	}

	dst := m.rt.ResolveTarget(dest)

	boot.AddAction(thread, boot.Action{
		Summary: fmt.Sprintf("git clone %s to %s", url, dst),
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			if _, err := os.Stat(filepath.Join(dst, ".git")); err == nil {
				return boot.ResultSkip, nil
			}

			if dryRun {
				return boot.ResultChange, nil
			}

			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return "", err
			}

			cmd := exec.CommandContext(ctx, "git", "clone", url, dst)
			if err := run(cmd); err != nil {
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

// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package fs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.starlark.net/starlark"
)

// Module returns the Starlark fs module.
func Module() boot.Module { return module{} }

type module struct{}

func (module) Name() string { return "fs" }

func (module) Members(rt *boot.Runtime) starlark.StringDict {
	m := &impl{rt: rt}
	return starlark.StringDict{
		"chmod":     starlark.NewBuiltin("fs.chmod", m.chmod),
		"dir":       starlark.NewBuiltin("fs.dir", m.dir),
		"symlink":   starlark.NewBuiltin("fs.symlink", m.symlink),
		"file":      starlark.NewBuiltin("fs.file", m.file),
		"newer":     starlark.NewBuiltin("fs.newer", m.newer),
		"remove":    starlark.NewBuiltin("fs.remove", m.remove),
		"sha256":    starlark.NewBuiltin("fs.sha256", m.sha256),
		"sync_tree": starlark.NewBuiltin("fs.sync_tree", m.syncTree),
		"template":  starlark.NewBuiltin("fs.template", m.template),
	}
}

type impl struct {
	rt *boot.Runtime
}

func (m *impl) chmod(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var (
		path string
		mode int
	)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "path", &path, "mode", &mode); err != nil {
		return nil, err
	}
	abs := m.rt.ResolveTarget(path)
	targetMode := os.FileMode(mode)
	boot.AddAction(thread, boot.Action{
		Summary: fmt.Sprintf("chmod %04o %s", targetMode.Perm(), abs),
		Apply: func(_ context.Context, dryRun bool) (boot.Result, error) {
			info, err := os.Stat(abs)
			if err != nil {
				return "", err
			}
			if info.Mode().Perm() == targetMode.Perm() {
				return boot.ResultSkip, nil
			}
			if dryRun {
				return boot.ResultChange, nil
			}
			if err := os.Chmod(abs, targetMode); err != nil {
				return "", err
			}
			return boot.ResultChange, nil
		},
	})
	return starlark.None, nil
}

func (m *impl) remove(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var path string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "path", &path); err != nil {
		return nil, err
	}
	abs := m.rt.ResolveTarget(path)
	boot.AddAction(thread, boot.Action{
		Summary: "remove " + abs,
		Apply: func(_ context.Context, dryRun bool) (boot.Result, error) {
			_, err := os.Lstat(abs)
			if errors.Is(err, fs.ErrNotExist) {
				return boot.ResultSkip, nil
			}
			if err != nil {
				return "", err
			}
			if dryRun {
				return boot.ResultChange, nil
			}
			if err := os.RemoveAll(abs); err != nil {
				return "", err
			}
			return boot.ResultChange, nil
		},
	})
	return starlark.None, nil
}

func (m *impl) syncTree(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var (
		source       string
		target       string
		owner        string
		group        string
		sudo         bool
		onlyIfExists bool
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"source", &source,
		"target", &target,
		"owner?", &owner,
		"group?", &group,
		"sudo?", &sudo,
		"only_if_exists?", &onlyIfExists,
	); err != nil {
		return nil, err
	}

	src := m.rt.ResolveSource(source)
	dst := m.rt.ResolveTarget(target)
	summary := fmt.Sprintf("sync tree %s to %s", src, dst)
	if owner != "" || group != "" {
		summary += fmt.Sprintf(" (owner %s:%s)", owner, group)
	}
	if sudo {
		summary += " (sudo)"
	}

	boot.AddAction(thread, boot.Action{
		Summary: summary,
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) && onlyIfExists {
				return boot.ResultSkip, nil
			} else if err != nil {
				return "", err
			}
			if dryRun && sudo && m.rt.NeedsSudo() {
				return boot.ResultChange, nil
			}

			args := []string{"-a"}
			if owner != "" || group != "" {
				args = append(args, "--chown="+owner+":"+group)
			}
			checkArgs := append([]string{"-ani"}, args[1:]...)
			checkArgs = append(checkArgs, withTrailingSeparator(src), withTrailingSeparator(dst))
			checkCmd := rsyncCommand(ctx, m.rt, sudo, checkArgs)
			out, err := checkCmd.CombinedOutput()
			if err != nil {
				return "", boot.CommandError(checkCmd.Args, out, err)
			}
			if len(bytes.TrimSpace(out)) == 0 {
				return boot.ResultSkip, nil
			}
			if dryRun {
				return boot.ResultChange, nil
			}

			args = append(args, withTrailingSeparator(src), withTrailingSeparator(dst))
			cmd := rsyncCommand(ctx, m.rt, sudo, args)
			out, err = cmd.CombinedOutput()
			if err != nil {
				return "", boot.CommandError(cmd.Args, out, err)
			}
			return boot.ResultChange, nil
		},
	})
	return starlark.None, nil
}

func withTrailingSeparator(path string) string {
	if strings.HasSuffix(path, string(os.PathSeparator)) {
		return path
	}
	return path + string(os.PathSeparator)
}

func rsyncCommand(ctx context.Context, rt *boot.Runtime, sudo bool, args []string) *exec.Cmd {
	if sudo && rt.NeedsSudo() {
		return exec.CommandContext(ctx, "sudo", append([]string{"rsync"}, args...)...)
	}
	return exec.CommandContext(ctx, "rsync", args...)
}

func (m *impl) file(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var (
		path    string
		content string
		mode    int = 0o644
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"path", &path,
		"content", &content,
		"mode?", &mode,
	); err != nil {
		return nil, err
	}
	abs := m.rt.ResolveTarget(path)
	boot.AddAction(thread, boot.Action{
		Summary: "file " + abs,
		Apply: func(_ context.Context, dryRun bool) (boot.Result, error) {
			targetMode := os.FileMode(mode)

			info, err := os.Stat(abs)
			if err == nil {
				got, err2 := os.ReadFile(abs)
				if err2 == nil && string(got) == content && info.Mode().Perm() == targetMode.Perm() {
					return boot.ResultSkip, nil
				}
			}

			if dryRun {
				return boot.ResultChange, nil
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(abs, []byte(content), targetMode); err != nil {
				return "", err
			}
			if err := os.Chmod(abs, targetMode); err != nil {
				return "", err
			}
			return boot.ResultChange, nil
		},
	})
	return starlark.None, nil
}

func (m *impl) template(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var (
		path   string
		text   string
		values *starlark.Dict
		mode   int = 0o644
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"path", &path,
		"template", &text,
		"values", &values,
		"mode?", &mode,
	); err != nil {
		return nil, err
	}
	content, err := renderTemplate(text, values)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.Name(), err)
	}
	return m.file(thread, starlark.NewBuiltin("fs.file", m.file), nil, []starlark.Tuple{
		{starlark.String("path"), starlark.String(path)},
		{starlark.String("content"), starlark.String(content)},
		{starlark.String("mode"), starlark.MakeInt(mode)},
	})
}

func renderTemplate(text string, values *starlark.Dict) (string, error) {
	for _, key := range values.Keys() {
		name, ok := starlark.AsString(key)
		if !ok {
			return "", fmt.Errorf("template value key %s is not a string", key)
		}
		value, _, _ := values.Get(key)
		replacement, ok := starlark.AsString(value)
		if !ok {
			replacement = value.String()
		}
		text = strings.ReplaceAll(text, "{{"+name+"}}", replacement)
	}
	return text, nil
}

func (m *impl) sha256(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "path", &path); err != nil {
		return nil, err
	}
	f, err := os.Open(m.rt.ResolveTarget(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return starlark.String(hex.EncodeToString(h.Sum(nil))), nil
}

func (m *impl) newer(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var source, target string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "source", &source, "target", &target); err != nil {
		return nil, err
	}
	src, err := os.Stat(m.rt.ResolveTarget(source))
	if err != nil {
		return nil, err
	}
	dst, err := os.Stat(m.rt.ResolveTarget(target))
	if errors.Is(err, fs.ErrNotExist) {
		return starlark.True, nil
	}
	if err != nil {
		return nil, err
	}
	return starlark.Bool(src.ModTime().After(dst.ModTime())), nil
}

func (m *impl) dir(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var path string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "path", &path); err != nil {
		return nil, err
	}
	abs := m.rt.ResolveTarget(path)
	boot.AddAction(thread, boot.Action{
		Summary: "dir " + abs,
		Apply: func(_ context.Context, dryRun bool) (boot.Result, error) {
			info, err := os.Stat(abs)
			switch {
			case err == nil && info.IsDir():
				return boot.ResultSkip, nil
			case err == nil:
				return "", fmt.Errorf("exists and is not a directory")
			case !errors.Is(err, fs.ErrNotExist):
				return "", err
			case dryRun:
				return boot.ResultChange, nil
			default:
				return boot.ResultChange, os.MkdirAll(abs, 0o755)
			}
		},
	})
	return starlark.None, nil
}

func (m *impl) symlink(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var (
		source string
		target string
		backup bool
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"source", &source,
		"target", &target,
		"backup?", &backup,
	); err != nil {
		return nil, err
	}
	src := m.rt.ResolveSource(source)
	dst := m.rt.ResolveTarget(target)

	boot.AddAction(thread, boot.Action{
		Summary: fmt.Sprintf("symlink %s -> %s", dst, src),
		Apply: func(_ context.Context, dryRun bool) (boot.Result, error) {
			info, err := os.Lstat(dst)
			if errors.Is(err, fs.ErrNotExist) {
				info = nil
			} else if err != nil {
				return "", err
			}
			if info != nil && info.Mode()&os.ModeSymlink != 0 {
				link, err := os.Readlink(dst)
				if err != nil {
					return "", err
				}
				if link == src {
					return boot.ResultSkip, nil
				}
			}

			if dryRun {
				return boot.ResultChange, nil
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return "", err
			}
			if err := replacePath(dst, backup); err != nil {
				return "", err
			}
			return boot.ResultChange, os.Symlink(src, dst)
		},
	})
	return starlark.None, nil
}

func replacePath(path string, backup bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if backup {
		return os.Rename(path, backupPath(path))
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func backupPath(path string) string {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	candidate := path + ".backup-" + stamp
	for i := 2; ; i++ {
		if _, err := os.Lstat(candidate); errors.Is(err, fs.ErrNotExist) {
			return candidate
		}
		candidate = fmt.Sprintf("%s.backup-%s-%d", path, stamp, i)
	}
}

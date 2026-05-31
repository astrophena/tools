// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package ssh

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

// Module returns the Starlark ssh module.
func Module() boot.Module { return module{} }

type module struct{}

func (module) Name() string { return "ssh" }

func (module) Members(rt *boot.Runtime) starlark.StringDict {
	m := &impl{rt: rt}
	return starlark.StringDict{
		"key": starlark.NewBuiltin("ssh.key", m.key),
	}
}

type impl struct {
	rt *boot.Runtime
}

func (m *impl) key(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var (
		path       string
		keyType    string = "ed25519"
		comment    string
		passphrase string
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"path", &path,
		"type?", &keyType,
		"comment?", &comment,
		"passphrase?", &passphrase,
	); err != nil {
		return nil, err
	}
	if comment == "" {
		hostname, err := m.rt.Hostname()
		if err != nil {
			return nil, err
		}
		comment = hostname
	}
	abs := m.rt.ResolveTarget(path)

	boot.AddAction(thread, boot.Action{
		Summary: fmt.Sprintf("ssh-keygen %s %s", keyType, abs),
		Apply: func(ctx context.Context, dryRun bool) (boot.Result, error) {
			if _, err := os.Stat(abs); err == nil {
				return boot.ResultSkip, nil
			} else if !errors.Is(err, fs.ErrNotExist) {
				return "", err
			}
			if dryRun {
				return boot.ResultChange, nil
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
				return "", err
			}
			cmd := exec.CommandContext(ctx, "ssh-keygen", "-q", "-t", keyType, "-C", comment, "-N", passphrase, "-f", abs)
			out, err := cmd.CombinedOutput()
			if err != nil {
				msg := strings.TrimSpace(string(out))
				if msg == "" {
					return "", err
				}
				return "", fmt.Errorf("%w:\n%s", err, msg)
			}
			return boot.ResultChange, nil
		},
	})
	return starlark.None, nil
}

// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package consent provides Starlark actions for interactive user consent.
package consent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.starlark.net/starlark"
)

// Module returns the Starlark consent module.
func Module() boot.Module { return module{} }

type module struct{}

func (module) Name() string { return "consent" }

func (module) Members(rt *boot.Runtime) starlark.StringDict {
	m := &impl{rt: rt}
	return starlark.StringDict{
		"require": starlark.NewBuiltin("consent.require", m.require),
	}
}

type impl struct {
	rt *boot.Runtime
}

func (m *impl) require(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !boot.InTask(thread) {
		return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
	}

	var (
		message    string
		defaultYes bool
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"message", &message,
		"default?", &defaultYes,
	); err != nil {
		return nil, err
	}
	if strings.TrimSpace(message) == "" {
		return nil, fmt.Errorf("%s: message cannot be empty", b.Name())
	}

	boot.AddAction(thread, boot.Action{
		Summary:   "would ask for consent: " + message,
		IsConsent: true,
		Apply: func(_ context.Context, dryRun bool) (boot.Result, error) {
			if dryRun {
				return boot.ResultChange, nil
			}
			if m.rt != nil && !m.rt.Interactive {
				return boot.ResultStop, nil
			}
			ok, err := ask(m.rt, message, defaultYes)
			if err != nil {
				return "", err
			}
			if !ok {
				return boot.ResultStop, nil
			}
			return boot.ResultSkip, nil
		},
	})
	return starlark.None, nil
}

func ask(rt *boot.Runtime, message string, defaultYes bool) (bool, error) {
	in := io.Reader(os.Stdin)
	out := io.Writer(os.Stdout)
	if rt != nil {
		if rt.Stdin != nil {
			in = rt.Stdin
		}
		if rt.Stdout != nil {
			out = rt.Stdout
		}
	}

	prompt := " [y/N] "
	if defaultYes {
		prompt = " [Y/n] "
	}
	if _, err := fmt.Fprint(out, message, prompt); err != nil {
		return false, err
	}

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	switch answer {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	case "":
		return defaultYes, nil
	default:
		return false, nil
	}
}

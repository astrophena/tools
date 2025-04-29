// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"errors"
	"fmt"
	"runtime/debug"

	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/cmd/starlet/internal/starlarkgemini"
	"go.astrophena.name/tools/cmd/starlet/internal/tgstarlark"
	"go.astrophena.name/tools/internal/starlark/go2star"
	"go.astrophena.name/tools/internal/util/tgmarkup"

	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

//go:generate ruff format --config stdlib/.ruff.toml --no-cache stdlib

// Starlark environment.

func (e *engine) predeclared() starlark.StringDict {
	return starlark.StringDict{
		"config": starlarkstruct.FromStringDict(
			starlarkstruct.Default,
			starlark.StringDict{
				"bot_id":       starlark.MakeInt64(e.tgBotID),
				"bot_username": starlark.String(e.tgBotUsername),
				"owner_id":     starlark.MakeInt64(e.tgOwner),
				"version":      starlark.String(version.Version().String()),
			},
		),
		"debug": starlarkstruct.FromStringDict(
			starlarkstruct.Default,
			starlark.StringDict{
				"stack":    starlark.NewBuiltin("debug.stack", starlarkDebugStack),
				"go_stack": starlark.NewBuiltin("debug.go_stack", starlarkDebugGoStack),
			},
		),
		"eval":    starlark.NewBuiltin("eval", starlarkEval),
		"fail":    starlark.NewBuiltin("fail", starlarkFail),
		"kvcache": e.kvCache,
		"files": &starlarkstruct.Module{
			Name: "files",
			Members: starlark.StringDict{
				"read": starlark.NewBuiltin("files.read", e.starlarkFilesRead),
			},
		},
		"gemini": starlarkgemini.Module(e.geminic),
		"markdown": &starlarkstruct.Module{
			Name: "markdown",
			Members: starlark.StringDict{
				"convert": starlark.NewBuiltin("markdown.convert", starlarkMarkdownConvert),
			},
		},
		"module":   starlark.NewBuiltin("module", starlarkstruct.MakeModule),
		"struct":   starlark.NewBuiltin("struct", starlarkstruct.Make),
		"telegram": tgstarlark.Module(e.tgToken, e.httpc),
		"time":     starlarktime.Module,
	}
}

// eval Starlark function.
func starlarkEval(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		code    string
		environ *starlark.Dict
	)
	if err := starlark.UnpackArgs(
		b.Name(),
		args, kwargs,
		"code", &code,
		"environ?", &environ,
	); err != nil {
		return nil, err
	}

	env := make(starlark.StringDict)
	if environ != nil {
		for key, val := range environ.Entries() {
			strk, ok := key.(starlark.String)
			if !ok {
				continue
			}
			env[string(strk)] = val
		}
	}

	var buf bytes.Buffer

	if _, err := starlark.ExecFileOptions(
		&syntax.FileOptions{},
		&starlark.Thread{
			Print: func(_ *starlark.Thread, msg string) { buf.WriteString(msg) },
		},
		"<eval>",
		code,
		env,
	); err != nil {
		return nil, err
	}

	return starlark.String(buf.String()), nil
}

// fail Starlark function.
func starlarkFail(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var errStr string
	if err := starlark.UnpackArgs(
		b.Name(),
		args, kwargs,
		"err", &errStr,
	); err != nil {
		return nil, err
	}
	return nil, errors.New(errStr)
}

// debug.stack Starlark function.
func starlarkDebugStack(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 || len(kwargs) > 0 {
		return nil, errors.New("unexpected arguments")
	}
	return starlark.String(thread.CallStack().String()), nil
}

// debug.go_stack Starlark function.
func starlarkDebugGoStack(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 || len(kwargs) > 0 {
		return nil, errors.New("unexpected arguments")
	}
	return starlark.String(string(debug.Stack())), nil
}

// files.read Starlark function.
func (e *engine) starlarkFilesRead(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name", &name); err != nil {
		return nil, err
	}
	file, ok := e.bot.Load().files[name]
	if !ok {
		return nil, fmt.Errorf("%s: no such file", name)
	}
	return starlark.String(file), nil
}

// markdown.convert Starlark function.
func starlarkMarkdownConvert(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "s", &s); err != nil {
		return nil, err
	}
	return go2star.To(tgmarkup.FromMarkdown(s))
}

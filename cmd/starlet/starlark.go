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
	"go.astrophena.name/tools/cmd/starlet/internal/tgmarkup"
	"go.astrophena.name/tools/cmd/starlet/internal/tgstarlark"
	"go.astrophena.name/tools/internal/starlark/go2star"

	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

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
				"stack":    starlark.NewBuiltin("debug.stack", getStarlarkStack),
				"go_stack": starlark.NewBuiltin("debug.go_stack", getGoStack),
			},
		),
		"eval":      starlark.NewBuiltin("eval", starlarkEval),
		"convcache": e.convCache,
		"files": &starlarkstruct.Module{
			Name: "files",
			Members: starlark.StringDict{
				"read": starlark.NewBuiltin("files.read", e.readFile),
			},
		},
		"gemini": starlarkgemini.Module(e.geminic),
		"markdown": &starlarkstruct.Module{
			Name: "markdown",
			Members: starlark.StringDict{
				"convert": starlark.NewBuiltin("markdown.convert", convertMarkdown),
			},
		},
		"module":   starlark.NewBuiltin("struct", starlarkstruct.MakeModule),
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

// debug.stack Starlark function.
func getStarlarkStack(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 || len(kwargs) > 0 {
		return nil, errors.New("unexpected arguments")
	}
	return starlark.String(thread.CallStack().String()), nil
}

// debug.go_stack Starlark function.
func getGoStack(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 || len(kwargs) > 0 {
		return nil, errors.New("unexpected arguments")
	}
	return starlark.String(string(debug.Stack())), nil
}

// markdown.convert Starlark function.
func convertMarkdown(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "s", &s); err != nil {
		return nil, err
	}
	return go2star.To(tgmarkup.FromMarkdown(s))
}

// files.read Starlark function.
func (e *engine) readFile(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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

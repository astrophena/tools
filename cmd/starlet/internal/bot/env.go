// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package bot

import (
	"errors"
	"fmt"
	"runtime/debug"

	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/internal/starlark/go2star"
	starlarkgemini "go.astrophena.name/tools/internal/starlark/lib/gemini"
	starlarktelegram "go.astrophena.name/tools/internal/starlark/lib/telegram"
	"go.astrophena.name/tools/internal/util/tgmarkup"

	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func (b *Bot) environment() starlark.StringDict {
	return starlark.StringDict{
		"config": starlarkstruct.FromStringDict(
			starlarkstruct.Default,
			starlark.StringDict{
				"bot_id":       starlark.MakeInt64(b.tgBotID),
				"bot_username": starlark.String(b.tgBotUsername),
				"owner_id":     starlark.MakeInt64(b.tgOwner),
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
		"fail": starlark.NewBuiltin("fail", starlarkFail),
		"files": &starlarkstruct.Module{
			Name: "files",
			Members: starlark.StringDict{
				"read": starlark.NewBuiltin("files.read", b.starlarkFilesRead),
			},
		},
		"gemini":  starlarkgemini.Module(b.geminic),
		"kvcache": b.kvCache,
		"markdown": &starlarkstruct.Module{
			Name: "markdown",
			Members: starlark.StringDict{
				"convert": starlark.NewBuiltin("markdown.convert", starlarkMarkdownConvert),
			},
		},
		"module":   starlark.NewBuiltin("module", starlarkstruct.MakeModule),
		"struct":   starlark.NewBuiltin("struct", starlarkstruct.Make),
		"telegram": starlarktelegram.Module(b.tgToken, b.httpc),
		"time":     starlarktime.Module,
	}
}

func starlarkDebugStack(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 || len(kwargs) > 0 {
		return nil, errors.New("unexpected arguments")
	}
	return starlark.String(thread.CallStack().String()), nil
}

func starlarkDebugGoStack(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 || len(kwargs) > 0 {
		return nil, errors.New("unexpected arguments")
	}
	return starlark.String(string(debug.Stack())), nil
}

func starlarkFail(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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

func (b *Bot) starlarkFilesRead(_ *starlark.Thread, built *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(built.Name(), args, kwargs, "name", &name); err != nil {
		return nil, err
	}
	file, ok := b.instance.Load().files[name]
	if !ok {
		return nil, fmt.Errorf("%s: no such file", name)
	}
	return starlark.String(file), nil
}

func starlarkMarkdownConvert(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "s", &s); err != nil {
		return nil, err
	}
	return go2star.To(tgmarkup.FromMarkdown(s))
}

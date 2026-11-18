// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package bot

import (
	"errors"
	"fmt"
	"runtime/debug"
	"strings"

	"go.astrophena.name/base/tgmarkup"
	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/internal/starlark/go2star"
	"go.astrophena.name/tools/internal/starlark/kvcache"
	"go.astrophena.name/tools/internal/starlark/llm"
	"go.astrophena.name/tools/internal/starlark/telegram"

	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

type environment []member

type member struct {
	Name    string
	Doc     string
	Args    []string
	Value   starlark.Value
	Members environment
}

func (d environment) stringDict() starlark.StringDict {
	dict := make(starlark.StringDict, len(d))
	for _, m := range d {
		val := m.Value
		if len(m.Members) > 0 {
			val = &starlarkstruct.Module{
				Name:    m.Name,
				Members: m.Members.stringDict(),
			}
		}
		dict[m.Name] = val
	}
	return dict
}

func (d environment) markdown() string {
	var b strings.Builder
	b.WriteString("# Starlark Environment\n\n")
	b.WriteString("These built-in functions and modules are available in the Starlark environment.\n\n")
	d.render(&b, 2, "")
	return strings.TrimSpace(b.String()) + "\n"
}

func (d environment) render(b *strings.Builder, level int, prefix string) {
	for _, m := range d {
		b.WriteString(strings.Repeat("#", level))
		b.WriteString(" `")
		b.WriteString(prefix)
		b.WriteString(m.Name)

		if _, ok := m.Value.(*starlark.Builtin); ok {
			b.WriteString("(")
			b.WriteString(strings.Join(m.Args, ", "))
			b.WriteString(")")
		}
		b.WriteString("`\n\n")

		b.WriteString(strings.TrimSpace(m.Doc))
		b.WriteString("\n\n")

		if len(m.Members) > 0 {
			m.Members.render(b, level+1, prefix+m.Name+".")
		}
	}
}

func (b *Bot) environment(files map[string]string, llmModule *starlarkstruct.Module) environment {
	return environment{
		{
			Name: "config",
			Doc:  "A module containing configuration information about the bot.",
			Members: environment{
				{
					Name:  "bot_id",
					Doc:   "The Telegram ID of the bot.",
					Value: starlark.MakeInt64(b.opts.BotID),
				},
				{
					Name:  "bot_username",
					Doc:   "The username of the bot.",
					Value: starlark.String(b.opts.BotUsername),
				},
				{
					Name:  "owner_id",
					Doc:   "The Telegram ID of the bot owner.",
					Value: starlark.MakeInt64(b.opts.Owner),
				},
				{
					Name:  "version",
					Doc:   "The version of the bot.",
					Value: starlark.String(version.Version().String()),
				},
			},
		},
		{
			Name: "debug",
			Doc:  "A module containing debugging utilities.",
			Members: environment{
				{
					Name:  "stack",
					Doc:   "Returns a string describing the current call stack.",
					Value: starlark.NewBuiltin("debug.stack", starlarkDebugStack),
				},
				{
					Name:  "go_stack",
					Doc:   "Returns a string describing the Go call stack.",
					Value: starlark.NewBuiltin("debug.go_stack", starlarkDebugGoStack),
				},
			},
		},
		{
			Name:  "fail",
			Doc:   "Terminates execution with a specified error message.",
			Args:  []string{"err: str"},
			Value: starlark.NewBuiltin("fail", starlarkFail),
		},
		{
			Name: "files",
			Doc:  "A module for accessing files provided to the bot.",
			Members: environment{
				{
					Name:  "read",
					Doc:   "Reads the content of a file.",
					Args:  []string{"name: str"},
					Value: filesReadBuiltin(files),
				},
			},
		},
		{
			Name:  "llm",
			Doc:   llm.Documentation(),
			Value: llmModule,
		},
		{
			Name:  "kvcache",
			Doc:   kvcache.Documentation(),
			Value: b.opts.KVCache,
		},
		{
			Name: "markdown",
			Doc:  "A module for Markdown conversion.",
			Members: environment{
				{
					Name:  "convert",
					Args:  []string{"s: str"},
					Doc:   "Converts a Markdown string to a Telegram message struct.",
					Value: starlark.NewBuiltin("markdown.convert", starlarkMarkdownConvert),
				},
			},
		},
		{
			Name:  "module",
			Args:  []string{"name: str", "**members"},
			Doc:   "Instantiates a module struct with the name from the specified keyword arguments.",
			Value: starlark.NewBuiltin("module", starlarkstruct.MakeModule),
		},
		{
			Name:  "struct",
			Args:  []string{"**fields"},
			Doc:   "Instantiates an immutable struct from the specified keyword arguments.",
			Value: starlark.NewBuiltin("struct", starlarkstruct.Make),
		},
		{
			Name:  "telegram",
			Doc:   telegram.Documentation(),
			Value: telegram.Module(b.opts.Token, b.opts.HTTPClient),
		},
		{
			Name:  "time",
			Doc:   "A module for time-related functions. See https://pkg.go.dev/go.starlark.net/lib/time#Module.",
			Value: starlarktime.Module,
		},
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

func filesReadBuiltin(files map[string]string) *starlark.Builtin {
	return starlark.NewBuiltin("files.read", func(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var name string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name", &name); err != nil {
			return nil, err
		}
		file, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("%s: no such file", name)
		}
		return starlark.String(file), nil
	})
}

func starlarkMarkdownConvert(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "s", &s); err != nil {
		return nil, err
	}
	return go2star.To(tgmarkup.FromMarkdown(s))
}

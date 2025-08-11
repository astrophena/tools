// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package bot

import (
	"errors"
	"fmt"
	"runtime/debug"
	"strings"

	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/internal/starlark/go2star"
	"go.astrophena.name/tools/internal/starlark/lib/gemini"
	"go.astrophena.name/tools/internal/starlark/lib/kvcache"
	"go.astrophena.name/tools/internal/starlark/lib/telegram"
	"go.astrophena.name/tools/internal/util/tgmarkup"

	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Environment is a collection of documented members of the Starlark environment.
type Environment []Member

// Member defines a documented Starlark environment member.
type Member struct {
	// Name is the name of the member.
	Name string
	// Doc is the documentation string for the member.
	Doc string
	// Value is the Starlark value. If Members is not nil, this should be nil,
	// as the value will be a module constructed from the members.
	Value starlark.Value
	// Members is a list of sub-members, used if this member is a module.
	Members []Member
}

// StringDict converts the Documentation into a [starlark.StringDict] that can be
// used as a global environment for a Starlark interpreter.
func (d Environment) StringDict() starlark.StringDict {
	dict := make(starlark.StringDict)
	for _, m := range d {
		var val starlark.Value
		if len(m.Members) > 0 {
			// This member is a module.
			val = &starlarkstruct.Module{
				Name:    m.Name,
				Members: Environment(m.Members).StringDict(),
			}
		} else {
			val = m.Value
		}
		dict[m.Name] = val
	}
	return dict
}

// Markdown generates a Markdown documentation string for the Starlark environment.
func (d Environment) Markdown() string {
	var b strings.Builder
	b.WriteString("# Starlark Environment\n\n")
	b.WriteString("These built-in functions and modules are available in the Starlark environment.\n\n")
	d.render(&b, 2, "")
	return strings.TrimSpace(b.String()) + "\n"
}

func (d Environment) render(b *strings.Builder, level int, prefix string) {
	for _, m := range d {
		b.WriteString(strings.Repeat("#", level))
		b.WriteString(" `")
		b.WriteString(prefix + m.Name)

		if _, ok := m.Value.(*starlark.Builtin); ok {
			b.WriteString("()")
		}
		b.WriteString("`\n\n")

		b.WriteString(strings.TrimSpace(m.Doc))
		b.WriteString("\n\n")

		if len(m.Members) > 0 {
			Environment(m.Members).render(b, level+1, prefix+m.Name+".")
		}
	}
}

func (b *Bot) environment() Environment {
	return Environment{
		{
			Name: "config",
			Doc:  "A module containing configuration information about the bot.",
			Members: []Member{
				{
					Name:  "bot_id",
					Doc:   "The Telegram ID of the bot.",
					Value: starlark.MakeInt64(b.tgBotID),
				},
				{
					Name:  "bot_username",
					Doc:   "The username of the bot.",
					Value: starlark.String(b.tgBotUsername),
				},
				{
					Name:  "owner_id",
					Doc:   "The Telegram ID of the bot owner.",
					Value: starlark.MakeInt64(b.tgOwner),
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
			Members: []Member{
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
			Value: starlark.NewBuiltin("fail", starlarkFail),
		},
		{
			Name: "files",
			Doc:  "A module for accessing files provided to the bot.",
			Members: []Member{
				{
					Name:  "read",
					Doc:   "Reads the content of a file.",
					Value: starlark.NewBuiltin("files.read", b.starlarkFilesRead),
				},
			},
		},
		{
			Name:  "gemini",
			Doc:   gemini.Documentation(),
			Value: gemini.Module(b.geminic),
		},
		{
			Name:  "kvcache",
			Doc:   kvcache.Documentation(),
			Value: b.kvCache,
		},
		{
			Name: "markdown",
			Doc:  "A module for Markdown conversion.",
			Members: []Member{
				{
					Name:  "convert",
					Doc:   "Converts a Markdown string to a Telegram message struct.",
					Value: starlark.NewBuiltin("markdown.convert", starlarkMarkdownConvert),
				},
			},
		},
		{
			Name:  "module",
			Doc:   "Instantiates a module struct with the name from the specified keyword arguments.",
			Value: starlark.NewBuiltin("module", starlarkstruct.MakeModule),
		},
		{
			Name:  "struct",
			Doc:   "Instantiates an immutable struct from the specified keyword arguments.",
			Value: starlark.NewBuiltin("struct", starlarkstruct.Make),
		},
		{
			Name:  "telegram",
			Doc:   telegram.Documentation(),
			Value: telegram.Module(b.tgToken, b.httpc),
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

// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"runtime/debug"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/unwrap"
	"go.astrophena.name/base/version"
	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/internal/starlark/go2star"
	"go.astrophena.name/tools/internal/starlark/interpreter"
	"go.astrophena.name/tools/internal/starlark/lib/gemini"
	"go.astrophena.name/tools/internal/starlark/lib/telegram"
	"go.astrophena.name/tools/internal/util/tgmarkup"

	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

const (
	mainFile          = "bot.star"
	errorTemplateFile = "error.tmpl"
)

var (
	errNoHandleFunc = errors.New("handle function not found in bot code")
	errNoMainFile   = errors.New(mainFile + " should contain bot code")
)

var (
	//go:embed static/templates/error.tmpl
	defaultErrorTemplate string
	//go:embed lib/*.star
	libRawFS embed.FS
	libFS    = unwrap.Value(fs.Sub(libRawFS, "lib"))
)

func (e *engine) loadFromGist(ctx context.Context) error {
	g, err := e.gistc.Get(ctx, e.gistID)
	if err != nil {
		return err
	}
	files := make(map[string]string)
	for name, file := range g.Files {
		files[name] = file.Content
	}
	return e.loadCode(ctx, files)
}

func (e *engine) loadCode(ctx context.Context, files map[string]string) error {
	_, exists := files[mainFile]
	if !exists {
		return errNoMainFile
	}

	intr := &interpreter.Interpreter{
		Predeclared: e.predeclared(),
		Logger: func(file string, line int, message string) {
			e.logf("%s:%d: %s", file, line, message)
		},
		Packages: map[string]interpreter.Loader{
			interpreter.MainPkg: interpreter.MemoryLoader(files),
			"starlet":           interpreter.FSLoader(libFS),
		},
	}
	if err := intr.Init(ctx); err != nil {
		return err
	}

	mod, err := intr.LoadModule(ctx, interpreter.MainPkg, mainFile)
	if err != nil {
		return err
	}
	if hook, ok := mod["on_load"]; ok {
		_, err = starlark.Call(intr.Thread(ctx), hook, starlark.Tuple{}, nil)
		if err != nil {
			return err
		}
	}

	newBot := &bot{
		files: files,
		intr:  intr,
	}
	if errorTmpl, exists := files[errorTemplateFile]; exists {
		newBot.errorTemplate = errorTmpl
	} else {
		newBot.errorTemplate = defaultErrorTemplate
	}
	e.bot.Store(newBot)

	return nil
}

func (e *engine) handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != e.tgSecret {
		web.RespondJSONError(w, r, web.ErrNotFound)
		return
	}

	b, err := io.ReadAll(r.Body)
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	var rawUpdate map[string]any
	if err := json.Unmarshal(b, &rawUpdate); err != nil {
		web.RespondJSONError(w, r, err)
		return
	}
	update, err := go2star.To(rawUpdate)
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	var (
		chatID = e.lookupChatID(rawUpdate) // for error reports
		bot    = e.bot.Load()
	)
	mod, err := bot.intr.LoadModule(r.Context(), interpreter.MainPkg, mainFile)
	if err != nil {
		e.reportError(r.Context(), chatID, w, err)
		return
	}
	f, ok := mod["handle"]
	if !ok {
		e.reportError(r.Context(), chatID, w, errNoHandleFunc)
		return
	}
	_, err = starlark.Call(bot.intr.Thread(r.Context()), f, starlark.Tuple{update}, nil)
	if err != nil {
		e.reportError(r.Context(), chatID, w, err)
		return
	}

	jsonOK(w)
}

func (e *engine) lookupChatID(update map[string]any) int64 {
	msg, ok := update["message"].(map[string]any)
	if !ok {
		return e.tgOwner
	}

	chat, ok := msg["chat"].(map[string]any)
	if !ok {
		return e.tgOwner
	}

	id, ok := chat["id"].(int64)
	if ok {
		return id
	}

	fid, ok := chat["id"].(float64)
	if ok {
		return int64(fid)
	}

	return e.tgOwner
}

// https://core.telegram.org/bots/api#linkpreviewoptions
type linkPreviewOptions struct {
	IsDisabled bool `json:"is_disabled"`
}

// https://core.telegram.org/bots/api#message
type message struct {
	tgmarkup.Message
	ChatID             int64              `json:"chat_id"`
	LinkPreviewOptions linkPreviewOptions `json:"link_preview_options"`
}

func (e *engine) reportError(ctx context.Context, chatID int64, w http.ResponseWriter, err error) {
	errMsg := err.Error()
	if evalErr, ok := err.(*starlark.EvalError); ok {
		errMsg = evalErr.Backtrace()
	}
	if e.scrubber != nil {
		// Mask secrets in error messages.
		errMsg = e.scrubber.Replace(errMsg)
	}

	msg := message{
		ChatID: chatID,
		LinkPreviewOptions: linkPreviewOptions{
			IsDisabled: true,
		},
	}

	errTmpl := e.bot.Load().errorTemplate
	if errTmpl == "" {
		errTmpl = defaultErrorTemplate
	}
	msg.Message = tgmarkup.FromMarkdown(fmt.Sprintf(errTmpl, errMsg))

	_, sendErr := request.Make[request.IgnoreResponse](ctx, request.Params{
		Method:     http.MethodPost,
		URL:        "https://api.telegram.org/bot" + e.tgToken + "/sendMessage",
		Body:       msg,
		HTTPClient: e.httpc,
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
		Scrubber: e.scrubber,
	})
	if sendErr != nil {
		e.logf("Reporting an error %q to bot owner (%q) failed: %v", err, e.tgOwner, sendErr)
	}

	// Don't respond with an error because Telegram will go mad and start retrying
	// updates until my Telegram chat is filled with lots of error messages.
	jsonOK(w)
}

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
		"fail": starlark.NewBuiltin("fail", starlarkFail),
		"files": &starlarkstruct.Module{
			Name: "files",
			Members: starlark.StringDict{
				"read": starlark.NewBuiltin("files.read", e.starlarkFilesRead),
			},
		},
		"gemini":  gemini.Module(e.geminic),
		"kvcache": e.kvCache,
		"markdown": &starlarkstruct.Module{
			Name: "markdown",
			Members: starlark.StringDict{
				"convert": starlark.NewBuiltin("markdown.convert", starlarkMarkdownConvert),
			},
		},
		"module":   starlark.NewBuiltin("module", starlarkstruct.MakeModule),
		"struct":   starlark.NewBuiltin("struct", starlarkstruct.Make),
		"telegram": telegram.Module(e.tgToken, e.httpc),
		"time":     starlarktime.Module,
	}
}

// debug.stack Starlark function.
func starlarkDebugStack(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 || len(kwargs) > 0 {
		return nil, errors.New("unexpected arguments")
	}
	return starlark.String(thread.CallStack().String()), nil
}

// debug.go_stack Starlark function.
func starlarkDebugGoStack(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) > 0 || len(kwargs) > 0 {
		return nil, errors.New("unexpected arguments")
	}
	return starlark.String(string(debug.Stack())), nil
}

// fail Starlark function.
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

// files.read Starlark function.
func (e *engine) starlarkFilesRead(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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
func starlarkMarkdownConvert(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "s", &s); err != nil {
		return nil, err
	}
	return go2star.To(tgmarkup.FromMarkdown(s))
}

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

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/version"
	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/cmd/starlet/internal/tgmarkup"
	"go.astrophena.name/tools/internal/starlark/go2star"
	"go.astrophena.name/tools/internal/starlark/interpreter"
	"go.astrophena.name/tools/internal/starlark/stdlib"

	"go.starlark.net/starlark"
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
	//go:embed assets/error.tmpl
	defaultErrorTemplate string
	//go:embed stdlib/*.star
	stdlibRawFS embed.FS
	stdlibFS    = must(fs.Sub(stdlibRawFS, "stdlib"))
)

func must[T any](val T, err error) T {
	if err != nil {
		panic(err)
	}
	return val
}

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
			interpreter.MainPkg:   interpreter.MemoryLoader(files),
			interpreter.StdlibPkg: stdlib.Loader(),
			"starlet":             interpreter.FSLoader(stdlibFS),
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
	var gu map[string]any
	if err := json.Unmarshal(b, &gu); err != nil {
		web.RespondJSONError(w, r, err)
		return
	}
	u, err := go2star.To(gu)
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	var (
		chatID = e.lookupChatID(gu) // for error reports
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
	_, err = starlark.Call(bot.intr.Thread(r.Context()), f, starlark.Tuple{u}, nil)
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

	_, sendErr := request.Make[any](ctx, request.Params{
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

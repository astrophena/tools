// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can befound in the LICENSE.md file.

// Package bot implements the core logic of the Starlet bot.
//
// It is responsible for handling Telegram webhooks, loading and executing
// Starlark code from a Gist, and providing a set of built-in functions
// to the Starlark environment.
package bot

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync/atomic"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/unwrap"
	"go.astrophena.name/base/version"
	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/internal/api/github/gist"
	"go.astrophena.name/tools/internal/api/google/gemini"
	"go.astrophena.name/tools/internal/starlark/go2star"
	"go.astrophena.name/tools/internal/starlark/interpreter"
	starlarkgemini "go.astrophena.name/tools/internal/starlark/lib/gemini"
	starlarktelegram "go.astrophena.name/tools/internal/starlark/lib/telegram"
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

// Bot represents a Starlet bot instance.
type Bot struct {
	tgToken       string
	tgSecret      string
	tgOwner       int64
	tgBotID       int64
	tgBotUsername string
	gistID        string

	httpc    *http.Client
	geminic  *gemini.Client
	gistc    *gist.Client
	kvCache  *starlarkstruct.Module
	scrubber *strings.Replacer
	logger   *slog.Logger

	instance atomic.Pointer[instance]
}

type instance struct {
	errorTemplate string
	files         map[string]string
	intr          *interpreter.Interpreter
}

// Opts is the options for creating a new Bot.
type Opts struct {
	// GistID is the ID of the Gist that contains the bot's code.
	GistID string
	// Token is the Telegram Bot API token.
	Token string
	// Secret is the Telegram Bot API secret token.
	Secret string
	// Owner is the Telegram ID of the bot owner.
	Owner int64
	// BotID is the Telegram ID of the bot.
	BotID int64
	// BotUsername is the username of the bot.
	BotUsername string
	// HTTPClient is the HTTP client to use for making requests.
	HTTPClient *http.Client
	// GistClient is the client for interacting with the GitHub Gist API.
	GistClient *gist.Client
	// GeminiClient is the client for interacting with the Google Gemini API.
	GeminiClient *gemini.Client
	// KVCache is the key-value cache for Starlark.
	KVCache *starlarkstruct.Module
	// Scrubber is used to scrub sensitive information from logs.
	Scrubber *strings.Replacer
	// Logger is the logger (FIXME: write more normal comment!).
	Logger *slog.Logger
}

// New creates a new Bot instance.
func New(opts Opts) *Bot {
	return &Bot{
		tgToken:       opts.Token,
		tgSecret:      opts.Secret,
		tgOwner:       opts.Owner,
		tgBotID:       opts.BotID,
		tgBotUsername: opts.BotUsername,
		gistID:        opts.GistID,
		httpc:         opts.HTTPClient,
		gistc:         opts.GistClient,
		geminic:       opts.GeminiClient,
		kvCache:       opts.KVCache,
		scrubber:      opts.Scrubber,
		logger:        opts.Logger,
	}
}

// LoadFromGist loads the bot's code from a Gist.
func (b *Bot) LoadFromGist(ctx context.Context) error {
	g, err := b.gistc.Get(ctx, b.gistID)
	if err != nil {
		return err
	}
	files := make(map[string]string)
	for name, file := range g.Files {
		files[name] = file.Content
	}
	return b.loadCode(ctx, files)
}

// Visited returns a list of modules visited by the interpreter.
func (b *Bot) Visited() []interpreter.ModuleKey {
	if inst := b.instance.Load(); inst != nil {
		return inst.intr.Visited()
	}
	return nil
}

func (b *Bot) loadCode(ctx context.Context, files map[string]string) error {
	_, exists := files[mainFile]
	if !exists {
		return errNoMainFile
	}

	starlarkLogger := b.logger.WithGroup("starlark")

	intr := &interpreter.Interpreter{
		Predeclared: b.predeclared(),
		Logger: func(file string, line int, message string) {
			starlarkLogger.Info(message, "file", file, "line", line)
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

	newInst := &instance{
		files: files,
		intr:  intr,
	}
	if errorTmpl, exists := files[errorTemplateFile]; exists {
		newInst.errorTemplate = errorTmpl
	} else {
		newInst.errorTemplate = defaultErrorTemplate
	}
	b.instance.Store(newInst)

	return nil
}

// HandleTelegramWebhook handles a Telegram webhook request.
func (b *Bot) HandleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != b.tgSecret {
		web.RespondJSONError(w, r, web.ErrNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	var rawUpdate map[string]any
	if err := json.Unmarshal(body, &rawUpdate); err != nil {
		web.RespondJSONError(w, r, err)
		return
	}
	update, err := go2star.To(rawUpdate)
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	var (
		chatID = b.lookupChatID(rawUpdate)
		inst   = b.instance.Load()
	)
	mod, err := inst.intr.LoadModule(r.Context(), interpreter.MainPkg, mainFile)
	if err != nil {
		b.reportError(r.Context(), chatID, w, err)
		return
	}
	f, ok := mod["handle"]
	if !ok {
		b.reportError(r.Context(), chatID, w, errNoHandleFunc)
		return
	}
	_, err = starlark.Call(inst.intr.Thread(r.Context()), f, starlark.Tuple{update}, nil)
	if err != nil {
		b.reportError(r.Context(), chatID, w, err)
		return
	}

	web.RespondJSON(w, ok)
}

var ok = map[string]string{
	"status": "ok",
}

func (b *Bot) lookupChatID(update map[string]any) int64 {
	msg, ok := update["message"].(map[string]any)
	if !ok {
		return b.tgOwner
	}

	chat, ok := msg["chat"].(map[string]any)
	if !ok {
		return b.tgOwner
	}

	id, ok := chat["id"].(int64)
	if ok {
		return id
	}

	fid, ok := chat["id"].(float64)
	if ok {
		return int64(fid)
	}

	return b.tgOwner
}

type linkPreviewOptions struct {
	IsDisabled bool `json:"is_disabled"`
}

type message struct {
	tgmarkup.Message
	ChatID             int64              `json:"chat_id"`
	LinkPreviewOptions linkPreviewOptions `json:"link_preview_options"`
}

func (b *Bot) reportError(ctx context.Context, chatID int64, w http.ResponseWriter, err error) {
	errMsg := err.Error()
	if evalErr, ok := err.(*starlark.EvalError); ok {
		errMsg = evalErr.Backtrace()
	}
	if b.scrubber != nil {
		errMsg = b.scrubber.Replace(errMsg)
	}

	msg := message{
		ChatID: chatID,
		LinkPreviewOptions: linkPreviewOptions{
			IsDisabled: true,
		},
	}

	errTmpl := b.instance.Load().errorTemplate
	if errTmpl == "" {
		errTmpl = defaultErrorTemplate
	}
	msg.Message = tgmarkup.FromMarkdown(fmt.Sprintf(errTmpl, errMsg))

	_, sendErr := request.Make[request.IgnoreResponse](ctx, request.Params{
		Method:     http.MethodPost,
		URL:        "https://api.telegram.org/bot" + b.tgToken + "/sendMessage",
		Body:       msg,
		HTTPClient: b.httpc,
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
		Scrubber: b.scrubber,
	})
	if sendErr != nil {
		b.logger.Error("reporting an error failed", "err", err, "bot_owner", b.tgOwner, "send_err", sendErr)
	}

	web.RespondJSON(w, ok)
}

func (b *Bot) predeclared() starlark.StringDict {
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

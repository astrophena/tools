// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package bot runs Starlet bots written in Starlark.
package bot

import (
	"cmp"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/tgmarkup"
	"go.astrophena.name/base/version"
	"go.astrophena.name/base/web"
	"go.astrophena.name/tools/internal/api/llm"
	"go.astrophena.name/tools/internal/starlark/go2star"
	"go.astrophena.name/tools/internal/starlark/interpreter"
	starllm "go.astrophena.name/tools/internal/starlark/llm"

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
	//go:embed error.tmpl
	defaultErrorTemplate string
	//go:embed lib/*.star
	libRawFS embed.FS
	libFS    = must(fs.Sub(libRawFS, "lib"))
)

func must[T any](val T, err error) T {
	if err != nil {
		panic(err)
	}
	return val
}

// Bot serves Telegram updates using the currently loaded Starlark code.
type Bot struct {
	opts     Opts
	llm      func() *starlarkstruct.Module
	instance atomic.Pointer[instance]
}

// instance holds the state that can be hot-reloaded.
type instance struct {
	errorTemplate string
	intr          *interpreter.Interpreter
	handle        starlark.Value
}

// Opts configures a Bot.
type Opts struct {
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
	// LLMClient is the client for interacting with an OpenAI-compatible LLM API.
	LLMClient *llm.Client
	// LLMUsagePath stores the llm module’s token counts.
	LLMUsagePath string
	// KVCache is the key-value cache for Starlark.
	KVCache *starlarkstruct.Module
	// Scrubber is used to scrub sensitive information from logs.
	Scrubber *strings.Replacer
	// Logger defaults to slog.Default.
	Logger *slog.Logger
}

// New creates a bot. Call Load before serving updates.
func New(opts Opts) *Bot {
	opts.Logger = cmp.Or(opts.Logger, slog.Default())
	return &Bot{
		opts: opts,
		llm: sync.OnceValue(func() *starlarkstruct.Module {
			return starllm.Module(opts.LLMClient, opts.LLMUsagePath)
		}),
	}
}

// Documentation returns Markdown help for the Starlark environment.
func (b *Bot) Documentation() string { return b.environment(nil, nil).markdown() }

// Visited returns a list of modules visited by the interpreter.
func (b *Bot) Visited() []interpreter.ModuleKey {
	if inst := b.instance.Load(); inst != nil {
		return inst.intr.Visited()
	}
	return nil
}

// Load loads the bot's Starlark code and assets from the provided files map.
func (b *Bot) Load(ctx context.Context, files map[string]string) error {
	if _, ok := files[mainFile]; !ok {
		return errNoMainFile
	}

	files = maps.Clone(files)
	starlarkLogger := b.opts.Logger.WithGroup("starlark")

	intr := &interpreter.Interpreter{
		Predeclared: b.environment(files, b.llm()).stringDict(),
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
		if _, err := starlark.Call(intr.Thread(ctx), hook, nil, nil); err != nil {
			return err
		}
	}

	b.instance.Store(&instance{
		intr:          intr,
		handle:        mod["handle"],
		errorTemplate: cmp.Or(files[errorTemplateFile], defaultErrorTemplate),
	})

	return nil
}

// HandleTelegramWebhook handles a Telegram webhook request.
func (b *Bot) HandleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != b.opts.Secret {
		web.RespondJSONError(w, r, web.ErrNotFound)
		return
	}

	var rawUpdate map[string]any
	if err := json.NewDecoder(r.Body).Decode(&rawUpdate); err != nil {
		web.RespondJSONError(w, r, err)
		return
	}
	update, err := go2star.To(rawUpdate)
	if err != nil {
		web.RespondJSONError(w, r, err)
		return
	}

	inst := b.instance.Load()
	if inst == nil {
		web.RespondJSONError(w, r, web.ErrInternalServerError)
		return
	}

	ctx := r.Context()
	if inst.handle == nil {
		b.reportError(ctx, inst.errorTemplate, b.lookupChatID(rawUpdate), w, errNoHandleFunc)
		return
	}
	if _, err := starlark.Call(inst.intr.Thread(ctx), inst.handle, starlark.Tuple{update}, nil); err != nil {
		b.reportError(ctx, inst.errorTemplate, b.lookupChatID(rawUpdate), w, err)
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
		return b.opts.Owner
	}

	chat, ok := msg["chat"].(map[string]any)
	if !ok {
		return b.opts.Owner
	}

	id, ok := chat["id"].(int64)
	if ok {
		return id
	}

	fid, ok := chat["id"].(float64)
	if ok {
		return int64(fid)
	}

	return b.opts.Owner
}

type linkPreviewOptions struct {
	IsDisabled bool `json:"is_disabled"`
}

type message struct {
	tgmarkup.Message
	ChatID             int64              `json:"chat_id"`
	LinkPreviewOptions linkPreviewOptions `json:"link_preview_options"`
}

func (b *Bot) reportError(ctx context.Context, tmpl string, chatID int64, w http.ResponseWriter, err error) {
	errMsg := err.Error()
	if evalErr, ok := err.(*starlark.EvalError); ok {
		errMsg = evalErr.Backtrace()
	}
	if b.opts.Scrubber != nil {
		errMsg = b.opts.Scrubber.Replace(errMsg)
	}

	msg := message{
		ChatID: chatID,
		LinkPreviewOptions: linkPreviewOptions{
			IsDisabled: true,
		},
	}

	msg.Message = tgmarkup.FromMarkdown(fmt.Sprintf(tmpl, errMsg))

	_, sendErr := request.Make[request.IgnoreResponse](ctx, request.Params{
		Method:     http.MethodPost,
		URL:        "https://api.telegram.org/bot" + b.opts.Token + "/sendMessage",
		Body:       msg,
		HTTPClient: b.opts.HTTPClient,
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
		Scrubber: b.opts.Scrubber,
	})
	if sendErr != nil {
		b.opts.Logger.Error("reporting an error failed", "err", err, "bot_owner", b.opts.Owner, "send_err", sendErr)
	}

	web.RespondJSON(w, ok)
}

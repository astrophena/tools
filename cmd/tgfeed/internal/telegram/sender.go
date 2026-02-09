// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package telegram implements message delivery over the Telegram Bot API.
package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/cmd/tgfeed/internal/sender"
	"go.astrophena.name/tools/internal/tgmarkup"
)

const (
	tgAPI          = "https://api.telegram.org"
	sendRetryLimit = 5 // N attempts to retry message sending
)

// Config configures a Telegram sender.
type Config struct {
	ChatID     string
	Token      string
	HTTPClient *http.Client
	Scrubber   *strings.Replacer
	Logger     *slog.Logger
}

// Sender sends messages via Telegram Bot API.
type Sender struct {
	chatID      string
	token       string
	httpc       *http.Client
	scrubber    *strings.Replacer
	slog        *slog.Logger
	makeRequest func(context.Context, string, any) error
	sleep       func(context.Context, time.Duration) bool
}

// New returns a Telegram sender configured for a specific chat.
func New(cfg Config) *Sender {
	s := &Sender{
		chatID:   cfg.ChatID,
		token:    cfg.Token,
		httpc:    cfg.HTTPClient,
		scrubber: cfg.Scrubber,
		slog:     cfg.Logger,
	}
	if s.httpc == nil {
		s.httpc = request.DefaultClient
	}
	if s.slog == nil {
		s.slog = slog.Default()
	}
	s.makeRequest = s.makeTelegramRequest
	s.sleep = sleep
	return s
}

type message struct {
	ChatID             string `json:"chat_id"`
	MessageThreadID    int64  `json:"message_thread_id,omitempty"`
	LinkPreviewOptions struct {
		IsDisabled bool `json:"is_disabled"`
	} `json:"link_preview_options"`
	ReplyMarkup *replyMarkup `json:"reply_markup,omitempty"`
	tgmarkup.Message
}

type replyMarkup struct {
	InlineKeyboard *inlineKeyboard `json:"inline_keyboard"`
}

type inlineKeyboard [][]inlineKeyboardButton

type inlineKeyboardButton struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

// Send sends a message to Telegram, retrying requests when rate limited.
func (s *Sender) Send(ctx context.Context, msg sender.Message) error {
	chatID := s.chatID
	if msg.Target.Channel != "" {
		chatID = msg.Target.Channel
	}

	tgmsg := &message{ChatID: chatID}
	if msg.Target.Topic != "" {
		threadID, err := strconv.ParseInt(msg.Target.Topic, 10, 64)
		if err != nil {
			return fmt.Errorf("parsing target topic as thread id: %w", err)
		}
		tgmsg.MessageThreadID = threadID
	}
	if len(msg.Actions) > 0 {
		tgmsg.ReplyMarkup = &replyMarkup{InlineKeyboard: toInlineKeyboard(msg.Actions)}
	}
	tgmsg.LinkPreviewOptions.IsDisabled = msg.Options.SuppressLinkPreview

	chunks := splitMessage(msg.Body)
	for _, chunk := range chunks {
		tgmsg.Message = tgmarkup.FromMarkdown(chunk)

		var err error
		for range sendRetryLimit {
			err = s.makeRequest(ctx, "sendMessage", tgmsg)
			if err == nil {
				break
			}

			retryable, wait := isRateLimited(err)
			if !retryable {
				break
			}

			s.slog.Warn("sending rate limited, waiting", slog.String("chat_id", chatID), slog.String("message", chunk), slog.Duration("wait", wait))
			if !s.sleep(ctx, wait) {
				return ctx.Err()
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func toInlineKeyboard(rows []sender.ActionRow) *inlineKeyboard {
	out := make(inlineKeyboard, 0, len(rows))
	for _, row := range rows {
		buttons := make([]inlineKeyboardButton, 0, len(row))
		for _, action := range row {
			if action.Label == "" || action.URL == "" {
				continue
			}
			buttons = append(buttons, inlineKeyboardButton{Text: action.Label, URL: action.URL})
		}
		if len(buttons) > 0 {
			out = append(out, buttons)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return &out
}

func (s *Sender) makeTelegramRequest(ctx context.Context, method string, args any) error {
	if _, err := request.Make[request.IgnoreResponse](ctx, request.Params{
		Method: http.MethodPost,
		URL:    tgAPI + "/bot" + s.token + "/" + method,
		Body:   args,
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
		HTTPClient: s.httpc,
		Scrubber:   s.scrubber,
	}); err != nil {
		return err
	}
	return nil
}

func splitMessage(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if utf8.RuneCountInString(text) <= 4096 {
		return []string{text}
	}

	var chunks []string
	for text != "" {
		if utf8.RuneCountInString(text) <= 4096 {
			chunks = append(chunks, text)
			break
		}

		var (
			lastNewline    = -1
			lastWhitespace = -1
			byteCap        = len(text)
			runeCount      int
		)

		for i, r := range text {
			if runeCount == 4096 {
				byteCap = i
				break
			}
			runeCount++

			if r == '\n' {
				lastNewline = i
				continue
			}
			if unicode.IsSpace(r) {
				lastWhitespace = i
			}
		}

		splitAt := byteCap
		switch {
		case lastNewline > 0:
			splitAt = lastNewline
		case lastWhitespace > 0:
			splitAt = lastWhitespace
		}

		chunk := strings.TrimSpace(text[:splitAt])
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		text = strings.TrimSpace(text[splitAt:])
	}

	return chunks
}

func isRateLimited(err error) (bool, time.Duration) {
	var statusErr *request.StatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusTooManyRequests {
		return false, 0
	}

	var errorResponse struct {
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(statusErr.Body, &errorResponse); err != nil {
		return false, 0
	}

	return true, time.Duration(errorResponse.Parameters.RetryAfter) * time.Second
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

var _ sender.Sender = (*Sender)(nil)

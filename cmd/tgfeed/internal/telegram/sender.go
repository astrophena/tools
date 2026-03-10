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

type captionPayload struct {
	Caption         string            `json:"caption,omitempty"`
	CaptionEntities []tgmarkup.Entity `json:"caption_entities,omitempty"`
}

func parseCaption(text string) captionPayload {
	if text == "" {
		return captionPayload{}
	}
	m := tgmarkup.FromMarkdown(text)
	return captionPayload{
		Caption:         m.Text,
		CaptionEntities: m.Entities,
	}
}

type sendMediaRequest struct {
	ChatID          string       `json:"chat_id"`
	MessageThreadID int64        `json:"message_thread_id,omitempty"`
	Photo           string       `json:"photo,omitempty"`
	Video           string       `json:"video,omitempty"`
	ReplyMarkup     *replyMarkup `json:"reply_markup,omitempty"`
	captionPayload
}

type inputMedia struct {
	Type  string `json:"type"`
	Media string `json:"media"`
	captionPayload
}

type sendMediaGroupRequest struct {
	ChatID          string       `json:"chat_id"`
	MessageThreadID int64        `json:"message_thread_id,omitempty"`
	Media           []inputMedia `json:"media"`
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
	var threadID int64
	if msg.Target.Topic != "" {
		tid, err := strconv.ParseInt(msg.Target.Topic, 10, 64)
		if err != nil {
			return fmt.Errorf("parsing target topic as thread id: %w", err)
		}
		threadID = tid
	}

	var replyMarkupStruct *replyMarkup
	if len(msg.Actions) > 0 {
		replyMarkupStruct = &replyMarkup{InlineKeyboard: toInlineKeyboard(msg.Actions)}
	}

	var chunks []string
	if len(msg.Media) > 0 {
		chunks = splitMessageCap(msg.Body, 1024)
	} else {
		chunks = splitMessageCap(msg.Body, 4096)
	}

	if len(msg.Media) > 1 {
		req := &sendMediaGroupRequest{
			ChatID:          chatID,
			MessageThreadID: threadID,
		}
		for i, m := range msg.Media {
			mediaType := "photo"
			if strings.Contains(m.Type, "video") || strings.HasSuffix(m.URL, ".mp4") {
				mediaType = "video"
			}
			im := inputMedia{Type: mediaType, Media: m.URL}
			if i == 0 && len(chunks) > 0 {
				im.captionPayload = parseCaption(chunks[0])
				chunks = chunks[1:] // consume the first chunk for the caption
			}
			req.Media = append(req.Media, im)
		}
		if err := s.doRequest(ctx, "sendMediaGroup", req); err != nil {
			return err
		}
	} else if len(msg.Media) == 1 {
		req := &sendMediaRequest{
			ChatID:          chatID,
			MessageThreadID: threadID,
			ReplyMarkup:     replyMarkupStruct, // single media supports reply markup
		}
		m := msg.Media[0]
		mediaType := "photo"
		if strings.Contains(m.Type, "video") || strings.HasSuffix(m.URL, ".mp4") {
			mediaType = "video"
		}
		if mediaType == "video" {
			req.Video = m.URL
		} else {
			req.Photo = m.URL
		}
		var method = "sendPhoto"
		if mediaType == "video" {
			method = "sendVideo"
		}
		if len(chunks) > 0 {
			req.captionPayload = parseCaption(chunks[0])
			chunks = chunks[1:]
		}
		if err := s.doRequest(ctx, method, req); err != nil {
			return err
		}
	}

	// Send remaining text chunks or standard text.
	for _, chunk := range chunks {
		tgmsg := &message{
			ChatID:          chatID,
			MessageThreadID: threadID,
			ReplyMarkup:     replyMarkupStruct,
		}
		tgmsg.LinkPreviewOptions.IsDisabled = msg.Options.SuppressLinkPreview
		tgmsg.Message = tgmarkup.FromMarkdown(chunk)
		if err := s.doRequest(ctx, "sendMessage", tgmsg); err != nil {
			return err
		}
	}

	return nil
}

func (s *Sender) doRequest(ctx context.Context, method string, req any) error {
	var err error
	for range sendRetryLimit {
		err = s.makeRequest(ctx, method, req)
		if err == nil {
			return nil
		}

		retryable, wait := isRateLimited(err)
		if !retryable {
			break
		}

		s.slog.Warn("sending rate limited, waiting", slog.String("method", method), slog.Duration("wait", wait))
		if !s.sleep(ctx, wait) {
			return ctx.Err()
		}
	}
	return err
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
	return splitMessageCap(text, 4096)
}

func splitMessageCap(text string, firstChunkCap int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if utf8.RuneCountInString(text) <= firstChunkCap {
		return []string{text}
	}

	var chunks []string
	limit := firstChunkCap
	for text != "" {
		if utf8.RuneCountInString(text) <= limit {
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
			if runeCount == limit {
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

		// All subsequent chunks will always be bounded by the standard 4096 length limit.
		limit = 4096
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

// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package telegram adapts tgfeed messages to the Telegram Bot API.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"go.astrophena.name/base/tgbot"
	"go.astrophena.name/base/version"
	"go.astrophena.name/tools/cmd/tgfeed/internal/sender"
)

// Config configures a Telegram sender.
type Config struct {
	ChatID     string
	Token      string
	HTTPClient *http.Client
	Logger     *slog.Logger
}

// Sender sends tgfeed messages through a Telegram bot.
type Sender struct {
	chatID string
	client *tgbot.Client
}

// New returns a sender configured for a default chat.
func New(cfg Config) *Sender {
	return &Sender{
		chatID: cfg.ChatID,
		client: tgbot.New(tgbot.Config{
			Token:      cfg.Token,
			HTTPClient: cfg.HTTPClient,
			Attempts:   5,
			UserAgent:  version.UserAgent(),
			Logger:     cfg.Logger,
		}),
	}
}

// Send sends msg to Telegram.
func (s *Sender) Send(ctx context.Context, msg sender.Message) error {
	chatID := s.chatID
	if msg.Target.Channel != "" {
		chatID = msg.Target.Channel
	}

	var threadID int64
	if msg.Target.Thread != "" {
		var err error
		threadID, err = strconv.ParseInt(msg.Target.Thread, 10, 64)
		if err != nil {
			return fmt.Errorf("parse target thread ID: %w", err)
		}
	}

	media := make([]tgbot.Media, 0, len(msg.Media))
	for _, m := range msg.Media {
		media = append(media, tgbot.Media{
			Type: m.Type,
			URL:  m.URL,
		})
	}

	_, err := s.client.Send(ctx, tgbot.Message{
		ChatID:             chatID,
		ThreadID:           threadID,
		Text:               msg.Body,
		Media:              media,
		Keyboard:           keyboard(msg.Actions),
		DisableLinkPreview: msg.Options.SuppressLinkPreview,
	})
	return err
}

func keyboard(rows []sender.ActionRow) [][]tgbot.Button {
	var out [][]tgbot.Button
	for _, row := range rows {
		var buttons []tgbot.Button
		for _, action := range row {
			if action.Label == "" || action.URL == "" {
				continue
			}
			buttons = append(buttons, tgbot.Button{
				Text: action.Label,
				URL:  action.URL,
			})
		}
		if len(buttons) > 0 {
			out = append(out, buttons)
		}
	}
	return out
}

var _ sender.Sender = (*Sender)(nil)

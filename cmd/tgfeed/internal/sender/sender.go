// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package sender defines a transport-agnostic message delivery interface.
package sender

import "context"

// Sender delivers messages to a configured destination.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// Message is a transport-agnostic outgoing message.
type Message struct {
	Text               string
	MessageThreadID    int64
	DisableLinkPreview bool
	InlineKeyboard     *InlineKeyboard
}

// InlineKeyboard is an optional matrix of buttons attached to the message.
type InlineKeyboard [][]InlineKeyboardButton

// InlineKeyboardButton is a URL button in an inline keyboard row.
type InlineKeyboardButton struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

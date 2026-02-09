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
	Body    string
	Target  Target
	Options Options
	Actions []ActionRow
}

// Target identifies where a message should be delivered.
type Target struct {
	Channel string
	Topic   string
}

// Options controls optional message delivery behavior.
type Options struct {
	SuppressLinkPreview bool
}

// ActionRow is a row of interactive actions.
type ActionRow []Action

// Action is an interactive message action.
type Action struct {
	Label string
	URL   string
}

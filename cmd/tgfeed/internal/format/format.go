// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package format renders tgfeed messages and decodes Starlark formatter results.
package format

import (
	"cmp"
	"fmt"
	urlpkg "net/url"
	"regexp"
	"strings"
	"sync"

	"go.astrophena.name/tools/cmd/tgfeed/internal/sender"
	"go.astrophena.name/tools/internal/starlark/go2star"

	"github.com/mmcdole/gofeed"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

var nonAlphaNumRe = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile("[^a-zA-Z0-9/]+")
})

// Feed carries formatting-relevant feed metadata.
type Feed struct {
	// URL is the feed source URL.
	URL string
	// Title is the human-readable feed title.
	Title string
	// Digest controls digest-style rendering.
	Digest bool
}

// Update contains feed metadata and items to render.
type Update struct {
	// Feed holds metadata for the current feed.
	Feed Feed
	// Items are parsed feed items to render.
	Items []*gofeed.Item
}

// Rendered is a fully rendered outgoing message.
type Rendered struct {
	// Body is the final message body.
	Body string
	// Actions are optional inline keyboard rows.
	Actions []sender.ActionRow
	// DisablePreview controls link preview rendering.
	DisablePreview bool
	// Media are optional media attachments (photos, videos).
	Media []sender.Media
}

// ValidationError indicates invalid formatter output.
type ValidationError struct {
	// Reason is a stable category, such as "invalid_type" or
	// "invalid_field_type".
	Reason string
	// ValueType is the unexpected Starlark type name.
	ValueType string
	// TupleLen is the invalid tuple length.
	TupleLen int
	// Field names the invalid logical field.
	Field string
	// FieldType is the unexpected type for Field.
	FieldType string
}

// Error returns a short diagnostic for invalid formatter output.
func (e *ValidationError) Error() string {
	switch e.Reason {
	case "invalid_type":
		return fmt.Sprintf("invalid value type %q", e.ValueType)
	case "invalid_tuple_length":
		return fmt.Sprintf("invalid tuple length %d", e.TupleLen)
	case "invalid_field_type":
		return fmt.Sprintf("invalid %s type %q", e.Field, e.FieldType)
	case "empty_title":
		return "title must not be empty"
	default:
		return "invalid format output"
	}
}

// BuildFormatInput builds formatter input for [CallStarlarkFormatter] and a
// fallback title for [DefaultUpdateMessage].
//
// Update.Items must contain at least one item.
func BuildFormatInput(u Update) (starlark.Value, string) {
	if u.Feed.Digest {
		list := make([]starlark.Value, 0, len(u.Items))
		for _, item := range u.Items {
			list = append(list, ItemToStarlark(item))
		}
		return starlark.NewList(list), fmt.Sprintf("Updates from %s", cmp.Or(u.Feed.Title, u.Feed.URL))
	}

	item := u.Items[0]
	return ItemToStarlark(item), cmp.Or(item.Title, item.Link)
}

// CallStarlarkFormatter evaluates the feed format function.
//
// Parse the returned value with [ParseFormattedMessage].
func CallStarlarkFormatter(formatFn *starlark.Function, items starlark.Value, print func(msg string)) (starlark.Value, error) {
	return starlark.Call(
		&starlark.Thread{
			Print: func(_ *starlark.Thread, msg string) { print(msg) },
		},
		formatFn,
		starlark.Tuple{items},
		nil,
	)
}

// ParseFormattedMessage decodes a string or a (text, keyboard?, media?) tuple.
// It returns a [ValidationError] for an invalid value.
func ParseFormattedMessage(v starlark.Value) (Rendered, error) {
	switch val := v.(type) {
	case starlark.String:
		return Rendered{
			Body: val.GoString(),
		}, nil
	case starlark.Tuple:
		if len(val) < 1 || len(val) > 3 {
			return Rendered{}, &ValidationError{
				Reason:   "invalid_tuple_length",
				TupleLen: len(val),
			}
		}

		s, ok := val[0].(starlark.String)
		if !ok {
			return Rendered{}, &ValidationError{
				Reason:    "invalid_field_type",
				Field:     "title",
				FieldType: val[0].Type(),
			}
		}
		if s.GoString() == "" {
			return Rendered{}, &ValidationError{
				Reason: "empty_title",
				Field:  "title",
			}
		}

		r := Rendered{
			Body: s.GoString(),
		}
		if len(val) >= 2 {
			actions, err := parseInlineKeyboard(val[1])
			if err != nil {
				return Rendered{}, err
			}
			r.Actions = actions
		}
		if len(val) >= 3 {
			media, err := parseMediaList(val[2])
			if err != nil {
				return Rendered{}, err
			}
			r.Media = media
		}
		return r, nil
	default:
		return Rendered{}, &ValidationError{
			Reason:    "invalid_type",
			ValueType: v.Type(),
		}
	}
}

func parseMediaList(v starlark.Value) ([]sender.Media, error) {
	list, ok := v.(*starlark.List)
	if !ok {
		return nil, &ValidationError{
			Reason:    "invalid_field_type",
			Field:     "media",
			FieldType: v.Type(),
		}
	}

	media := make([]sender.Media, 0, list.Len())
	for mediaValue := range list.Elements() {
		mediaDict, ok := mediaValue.(*starlark.Dict)
		if !ok {
			continue
		}
		kind := stringField(mediaDict, "type")
		url := stringField(mediaDict, "url")
		if kind != "" && url != "" {
			media = append(media, sender.Media{Type: kind, URL: url})
		}
	}

	return media, nil
}

func parseInlineKeyboard(v starlark.Value) ([]sender.ActionRow, error) {
	list, ok := v.(*starlark.List)
	if !ok {
		return nil, &ValidationError{
			Reason:    "invalid_field_type",
			Field:     "keyboard",
			FieldType: v.Type(),
		}
	}

	rows := make([]sender.ActionRow, 0, list.Len())
	for rowValue := range list.Elements() {
		rowList, ok := rowValue.(*starlark.List)
		if !ok {
			continue
		}

		buttons := make([]sender.Action, 0, rowList.Len())
		for buttonValue := range rowList.Elements() {
			buttonDict, ok := buttonValue.(*starlark.Dict)
			if !ok {
				continue
			}
			if button, ok := parseInlineKeyboardButton(buttonDict); ok {
				buttons = append(buttons, button)
			}
		}
		if len(buttons) > 0 {
			rows = append(rows, buttons)
		}
	}

	return rows, nil
}

func parseInlineKeyboardButton(button *starlark.Dict) (sender.Action, bool) {
	label := stringField(button, "text")
	url := stringField(button, "url")
	return sender.Action{Label: label, URL: url}, label != "" && url != ""
}

func stringField(dict *starlark.Dict, key string) string {
	value, _, err := dict.Get(starlark.String(key))
	if err != nil {
		return ""
	}
	s, _ := starlark.AsString(value)
	return s
}

// DefaultUpdateMessage renders a message for feeds without a custom formatter.
func DefaultUpdateMessage(u Update, defaultTitle, messageTemplate string) Rendered {
	if u.Feed.Digest {
		var msg strings.Builder
		fmt.Fprintf(&msg, "<b>%s</b>\n\n", defaultTitle)
		for _, item := range u.Items {
			fmt.Fprintf(&msg, "• <a href=%q>%s</a>\n", item.Link, cmp.Or(item.Title, item.Link))
		}
		return Rendered{
			Body:           msg.String(),
			DisablePreview: true,
		}
	}

	item := u.Items[0]
	msg := fmt.Sprintf(messageTemplate, defaultTitle, item.Link)
	if url, err := urlpkg.Parse(item.Link); err == nil {
		switch url.Hostname() {
		case "t.me":
			msg += " #tg"
		case "www.youtube.com":
			msg += " #youtube"
		default:
			msg += " #" + nonAlphaNumRe().ReplaceAllString(url.Hostname(), "")
		}
	}

	var actions []sender.ActionRow
	if strings.HasPrefix(item.GUID, "https://news.ycombinator.com/item?id=") {
		actions = []sender.ActionRow{
			{
				{
					Label: "↪ Hacker News",
					URL:   item.GUID,
				},
			},
		}
	}

	return Rendered{
		Body:    msg,
		Actions: actions,
	}
}

// ItemToStarlark converts an RSS item into a Starlark struct used by feed
// formatter functions.
func ItemToStarlark(item *gofeed.Item) starlark.Value {
	categories := make([]starlark.Value, 0, len(item.Categories))
	for _, category := range item.Categories {
		categories = append(categories, starlark.String(category))
	}
	enclosures := make([]starlark.Value, 0, len(item.Enclosures))
	for _, enc := range item.Enclosures {
		enclosures = append(enclosures, starlarkstruct.FromStringDict(
			starlarkstruct.Default,
			starlark.StringDict{
				"url":    starlark.String(enc.URL),
				"type":   starlark.String(enc.Type),
				"length": starlark.String(enc.Length),
			}))
	}
	extensions, _ := go2star.To(item.Extensions)
	return starlarkstruct.FromStringDict(
		starlarkstruct.Default,
		starlark.StringDict{
			"title":       starlark.String(item.Title),
			"url":         starlark.String(item.Link),
			"description": starlark.String(item.Description),
			"content":     starlark.String(item.Content),
			"categories":  starlark.NewList(categories),
			"enclosures":  starlark.NewList(enclosures),
			"extensions":  extensions,
			"guid":        starlark.String(item.GUID),
			"published":   starlark.String(item.Published),
		},
	)
}

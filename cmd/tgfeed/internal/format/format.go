// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package format builds and validates tgfeed message render output.
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
	URL    string
	Title  string
	Digest bool
}

// Update contains feed metadata and items to render.
type Update struct {
	Feed  Feed
	Items []*gofeed.Item
}

// Rendered is a fully rendered outgoing message.
type Rendered struct {
	Body           string
	Actions        []sender.ActionRow
	DisablePreview bool
}

// ValidationError indicates invalid formatter output.
type ValidationError struct {
	Reason    string
	ValueType string
	TupleLen  int
	Field     string
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

// BuildFormatInput builds starlark formatter input and fallback title.
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
func CallStarlarkFormatter(formatFn *starlark.Function, items starlark.Value, print func(msg string)) (starlark.Value, error) {
	return starlark.Call(
		&starlark.Thread{Print: func(_ *starlark.Thread, msg string) { print(msg) }},
		formatFn,
		starlark.Tuple{items},
		[]starlark.Tuple{},
	)
}

// ParseFormattedMessage validates and parses formatter output.
func ParseFormattedMessage(v starlark.Value) (Rendered, error) {
	switch val := v.(type) {
	case starlark.String:
		return Rendered{Body: val.GoString()}, nil
	case starlark.Tuple:
		if len(val) < 1 || len(val) > 2 {
			return Rendered{}, &ValidationError{Reason: "invalid_tuple_length", TupleLen: len(val)}
		}

		s, ok := val[0].(starlark.String)
		if !ok {
			return Rendered{}, &ValidationError{Reason: "invalid_field_type", Field: "title", FieldType: val[0].Type()}
		}
		if s.GoString() == "" {
			return Rendered{}, &ValidationError{Reason: "empty_title", Field: "title"}
		}

		r := Rendered{Body: s.GoString()}
		if len(val) == 2 {
			actions, err := parseInlineKeyboard(val[1])
			if err != nil {
				return Rendered{}, err
			}
			r.Actions = actions
		}
		return r, nil
	default:
		return Rendered{}, &ValidationError{Reason: "invalid_type", ValueType: v.Type()}
	}
}

func parseInlineKeyboard(v starlark.Value) ([]sender.ActionRow, error) {
	list, ok := v.(*starlark.List)
	if !ok {
		return nil, &ValidationError{Reason: "invalid_field_type", Field: "keyboard", FieldType: v.Type()}
	}

	rows := make([]sender.ActionRow, 0, list.Len())
	iter := list.Iterate()
	defer iter.Done()

	var rowValue starlark.Value
	for iter.Next(&rowValue) {
		rowList, ok := rowValue.(*starlark.List)
		if !ok {
			continue
		}

		buttons := make([]sender.Action, 0, rowList.Len())
		rowIter := rowList.Iterate()
		var buttonValue starlark.Value
		for rowIter.Next(&buttonValue) {
			buttonDict, ok := buttonValue.(*starlark.Dict)
			if !ok {
				continue
			}
			if button, ok := parseInlineKeyboardButton(buttonDict); ok {
				buttons = append(buttons, button)
			}
		}
		rowIter.Done()

		if len(buttons) > 0 {
			rows = append(rows, buttons)
		}
	}

	return rows, nil
}

func parseInlineKeyboardButton(button *starlark.Dict) (sender.Action, bool) {
	var out sender.Action
	for _, item := range button.Items() {
		key, ok1 := item[0].(starlark.String)
		val, ok2 := item[1].(starlark.String)
		if !ok1 || !ok2 {
			continue
		}

		switch key.GoString() {
		case "text":
			out.Label = val.GoString()
		case "url":
			out.URL = val.GoString()
		}
	}

	if out.Label == "" || out.URL == "" {
		return sender.Action{}, false
	}
	return out, true
}

// DefaultUpdateMessage renders the built-in fallback message.
func DefaultUpdateMessage(u Update, defaultTitle string, messageTemplate string) Rendered {
	if u.Feed.Digest {
		msg := fmt.Sprintf("<b>%s</b>\n\n", defaultTitle)
		for _, item := range u.Items {
			msg += fmt.Sprintf("• <a href=%q>%s</a>\n", item.Link, cmp.Or(item.Title, item.Link))
		}
		return Rendered{Body: msg, DisablePreview: true}
	}

	msg := fmt.Sprintf(messageTemplate, defaultTitle, u.Items[0].Link)
	if u, err := urlpkg.Parse(u.Items[0].Link); err == nil {
		switch u.Hostname() {
		case "t.me":
			msg += " #tg"
		case "www.youtube.com":
			msg += " #youtube"
		default:
			msg += " #" + nonAlphaNumRe().ReplaceAllString(u.Hostname(), "")
		}
	}

	var actions []sender.ActionRow
	if strings.HasPrefix(u.Items[0].GUID, "https://news.ycombinator.com/item?id=") {
		actions = []sender.ActionRow{{
			{Label: "↪ Hacker News", URL: u.Items[0].GUID},
		}}
	}

	return Rendered{Body: msg, Actions: actions, DisablePreview: u.Feed.Digest}
}

// ItemToStarlark converts an RSS item into a starlark item object.
func ItemToStarlark(item *gofeed.Item) starlark.Value {
	var categories []starlark.Value
	for _, category := range item.Categories {
		categories = append(categories, starlark.String(category))
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
			"extensions":  extensions,
			"guid":        starlark.String(item.GUID),
			"published":   starlark.String(item.Published),
		},
	)
}

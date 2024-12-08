// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package tgmarkup provides functionality to convert Markdown text to
// Telegram-flavored message markup.
package tgmarkup

import (
	"regexp"
	"strings"
	"sync"
	"unicode/utf16"

	"rsc.io/markdown"
)

// Message represents a Telegram message with text and entities for formatting.
// It is designed to be marshaled into JSON for use with the Telegram Bot API.
// See https://core.telegram.org/bots/api#message for more information.
type Message struct {
	Text     string   `json:"text" starlark:"text"`
	Entities []Entity `json:"entities" starlark:"entities"`
}

// Type represents a Telegram message entity type.
type Type string

// Constants for various Telegram message entity types.
// See https://core.telegram.org/bots/api#messageentity for a complete list of
// supported types.
const (
	Mention              Type = "mention"      // @username
	Hashtag              Type = "hashtag"      // #hashtag
	Cashtag              Type = "cashtag"      // $USD
	BotCommand           Type = "bot_command"  // /start@jobs_bot
	URL                  Type = "url"          // https://telegram.org
	Email                Type = "email"        // do-not-reply@telegram.org
	PhoneNumber          Type = "phone_number" // +1-212-555-0123
	Bold                 Type = "bold"
	Italic               Type = "italic"
	Underline            Type = "underline"
	Strikethrough        Type = "strikethrough"
	Spoiler              Type = "spoiler"
	Blockquote           Type = "blockquote"
	ExpandableBlockquote Type = "expandable_blockquote"
	Code                 Type = "code" // monowidth string
	Pre                  Type = "pre"  // monowidth block
	TextLink             Type = "text_link"
	TextMention          Type = "text_mention"
	CustomEmoji          Type = "custom_emoji"
)

// Entity defines the type and location of a formatted part of the message text.
// See https://core.telegram.org/bots/api#messageentity.
type Entity struct {
	Type Type `json:"type" starlark:"type"`
	// Offset in UTF-16 code units to the start of the entity.
	Offset int `json:"offset" starlark:"offset"`
	// Length of the entity in UTF-16 code units.
	Length int `json:"length" starlark:"length"`
	// Optional. For “text_link” only, URL that will be opened after user taps on
	// the text.
	URL string `json:"url,omitempty" starlark:"url"`
	// Optional. For “pre” only, the programming language of the entity text.
	Language string `json:"language,omitempty" starlark:"language"`
}

var parser = sync.OnceValue(func() *markdown.Parser {
	return &markdown.Parser{
		Strikethrough:      true,
		TaskList:           false,
		AutoLinkText:       true,
		AutoLinkAssumeHTTP: true,
		Table:              false,
		SmartDot:           true,
		SmartDash:          true,
		SmartQuote:         true,
	}
})

// FromMarkdown converts a Markdown text to a [Message].
func FromMarkdown(text string) Message {
	md := parser().Parse(text)

	var sb strings.Builder
	var entities []Entity

	for _, b := range md.Blocks {
		convertBlock(b, &sb, &entities)
	}

	return Message{
		Text:     sb.String(),
		Entities: entities,
	}
}

var whiteSpaceRe = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`[ ]+`)
})

func cleanWhitespace(text string) string {
	return whiteSpaceRe().ReplaceAllString(text, " ")
}

func convertBlock(b markdown.Block, sb *strings.Builder, entities *[]Entity) {
	switch block := b.(type) {
	case *markdown.Paragraph:
		convertInlines(block.Text.Inline, sb, entities)
		sb.WriteString("\n")
	case *markdown.Text:
		// This is a Block for tight list items.
		sb.WriteString("• ")
		convertInlines(block.Inline, sb, entities)
		sb.WriteString("\n")
	case *markdown.Quote:
		offset := utf16len(sb.String())
		for _, block := range block.Blocks {
			convertBlock(block, sb, entities)
		}
		*entities = append(*entities, Entity{
			Type:   Blockquote,
			Offset: offset,
			Length: utf16len(sb.String()) - offset,
		})
	case *markdown.CodeBlock:
		offset := utf16len(sb.String())
		for _, line := range block.Text {
			sb.WriteString(line)
			sb.WriteString("\n")
		}

		entity := Entity{
			Type:   Pre,
			Offset: offset,
			Length: utf16len(sb.String()) - offset - 1,
		}
		if block.Info != "" {
			entity.Language = block.Info
		}
		*entities = append(*entities, entity)

	case *markdown.Heading:
		offset := utf16len(sb.String())
		convertInlines(block.Text.Inline, sb, entities)
		sb.WriteString("\n")
		*entities = append(*entities, Entity{
			Type:   Bold,
			Offset: offset,
			Length: utf16len(sb.String()) - offset - 1,
		})
	case *markdown.List:
		for _, itemBlock := range block.Items {
			item := itemBlock.(*markdown.Item)
			for _, b := range item.Blocks {
				convertBlock(b, sb, entities)
			}
		}
	case *markdown.ThematicBreak:
		sb.WriteString("⸻\n")
	}
}

func convertInlines(inlines markdown.Inlines, sb *strings.Builder, entities *[]Entity) {
	for _, inline := range inlines {
		convertInline(inline, sb, entities)
	}
}

func convertInline(i markdown.Inline, sb *strings.Builder, entities *[]Entity) {
	switch inline := i.(type) {
	case *markdown.Plain:
		// Remove 2+ spaces in plain text before writing it.
		// See https://old.reddit.com/r/GoogleGeminiAI/comments/1d1z9l3/google_gemini_15_output_contains_random_extra/.
		sb.WriteString(cleanWhitespace(inline.Text))
	case *markdown.Strong:
		offset := utf16len(sb.String())
		convertInlines(inline.Inner, sb, entities)
		*entities = append(*entities, Entity{
			Type:   Bold,
			Offset: offset,
			Length: utf16len(sb.String()) - offset,
		})
	case *markdown.Emph:
		offset := utf16len(sb.String())
		convertInlines(inline.Inner, sb, entities)
		*entities = append(*entities, Entity{
			Type:   Italic,
			Offset: offset,
			Length: utf16len(sb.String()) - offset,
		})
	case *markdown.Link:
		offset := utf16len(sb.String())
		convertInlines(inline.Inner, sb, entities)
		*entities = append(*entities, Entity{
			Type:   TextLink,
			Offset: offset,
			Length: utf16len(sb.String()) - offset,
			URL:    inline.URL,
		})
	case *markdown.AutoLink:
		offset := utf16len(sb.String())
		sb.WriteString(cleanWhitespace(inline.Text))
		*entities = append(*entities, Entity{
			Type:   URL,
			Offset: offset,
			Length: utf16len(sb.String()) - offset,
		})
	case *markdown.Code:
		offset := utf16len(sb.String())
		sb.WriteString(inline.Text)
		*entities = append(*entities, Entity{
			Type:   Code,
			Offset: offset,
			Length: utf16len(sb.String()) - offset,
		})
	case *markdown.Del:
		offset := utf16len(sb.String())
		convertInlines(inline.Inner, sb, entities)
		*entities = append(*entities, Entity{
			Type:   Strikethrough,
			Offset: offset,
			Length: utf16len(sb.String()) - offset,
		})
	case *markdown.SoftBreak:
		sb.WriteString("\n")
	case *markdown.HardBreak:
		sb.WriteString("\n")
	}
}

func utf16len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

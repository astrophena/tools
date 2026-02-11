// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package format

import (
	"strings"
	"testing"

	"github.com/mmcdole/gofeed"
	"go.astrophena.name/base/testutil"
	"go.starlark.net/starlark"
)

func TestParseFormattedMessage(t *testing.T) {
	t.Parallel()

	keyboard := starlark.NewList([]starlark.Value{
		starlark.NewList([]starlark.Value{starlark.NewDict(2)}),
	})
	dict := keyboard.Index(0).(*starlark.List).Index(0).(*starlark.Dict)
	dict.SetKey(starlark.String("text"), starlark.String("Open"))
	dict.SetKey(starlark.String("url"), starlark.String("https://example.com"))

	cases := map[string]struct {
		input      starlark.Value
		wantBody   string
		wantAction bool
		wantReason string
	}{
		"string": {
			input:    starlark.String("hello"),
			wantBody: "hello",
		},
		"tuple with keyboard": {
			input:      starlark.Tuple{starlark.String("formatted"), keyboard},
			wantBody:   "formatted",
			wantAction: true,
		},
		"tuple malformed first element": {
			input:      starlark.Tuple{starlark.MakeInt(1)},
			wantReason: "invalid_field_type",
		},
		"tuple empty title": {
			input:      starlark.Tuple{starlark.String("")},
			wantReason: "empty_title",
		},
		"tuple malformed keyboard": {
			input:      starlark.Tuple{starlark.String("formatted"), starlark.MakeInt(1)},
			wantReason: "invalid_field_type",
		},
		"tuple invalid length": {
			input:      starlark.Tuple{starlark.String("one"), starlark.NewList(nil), starlark.String("three")},
			wantReason: "invalid_tuple_length",
		},
		"unsupported": {
			input:      starlark.MakeInt(1),
			wantReason: "invalid_type",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ParseFormattedMessage(tc.input)
			if tc.wantReason != "" {
				if err == nil {
					t.Fatal("expected validation error")
				}
				vErr, ok := err.(*ValidationError)
				if !ok {
					t.Fatalf("expected ValidationError, got %T", err)
				}
				testutil.AssertEqual(t, vErr.Reason, tc.wantReason)
				return
			}

			testutil.AssertEqual(t, err, nil)
			testutil.AssertEqual(t, got.Body, tc.wantBody)
			testutil.AssertEqual(t, len(got.Actions) > 0, tc.wantAction)
			if tc.wantAction {
				testutil.AssertEqual(t, got.Actions[0][0].Label, "Open")
				testutil.AssertEqual(t, got.Actions[0][0].URL, "https://example.com")
			}
		})
	}
}

func TestBuildFormatInput(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		update    Update
		wantTitle string
	}{
		"single item uses fallback title": {
			update: Update{
				Feed:  Feed{},
				Items: []*gofeed.Item{{Title: "", Link: "https://example.com/a"}},
			},
			wantTitle: "https://example.com/a",
		},
		"digest uses feed URL when title is empty": {
			update: Update{
				Feed:  Feed{Digest: true, URL: "https://example.com/feed.xml"},
				Items: []*gofeed.Item{{Title: "Item", Link: "https://example.com/a"}},
			},
			wantTitle: "Updates from https://example.com/feed.xml",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, gotTitle := BuildFormatInput(tc.update)
			testutil.AssertEqual(t, gotTitle, tc.wantTitle)
		})
	}
}

func TestDefaultUpdateMessage(t *testing.T) {
	t.Parallel()

	rendered := DefaultUpdateMessage(Update{Feed: Feed{}, Items: []*gofeed.Item{{
		Title: "Title",
		Link:  "https://example.com/post",
		GUID:  "https://news.ycombinator.com/item?id=1",
	}}}, "Title", "%s\n%s")

	testutil.AssertEqual(t, rendered.Actions[0][0].Label, "↪ Hacker News")
	testutil.AssertEqual(t, rendered.Actions[0][0].URL, "https://news.ycombinator.com/item?id=1")
}

func TestValidationErrorError(t *testing.T) {
	t.Parallel()

	err := (&ValidationError{Reason: "invalid_tuple_length", TupleLen: 3}).Error()
	if !strings.Contains(err, "3") {
		t.Fatalf("unexpected error string: %q", err)
	}
}

// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	_ "embed"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
	"go.astrophena.name/base/syncx"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.starlark.net/starlark"
)

func TestFailingFeed(t *testing.T) {
	t.Parallel()

	tm := testMux(t, txtarToFS(txtar.Parse(defaultGistTxtar)), map[string]http.HandlerFunc{
		atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "I'm a teapot.", http.StatusTeapot)
		},
	})
	f := testFetcher(t, tm)
	if err := f.run(t.Context()); err != nil {
		t.Fatal(err)
	}

	state := tm.state(t)

	testutil.AssertEqual(t, state[atomFeedURL].ErrorCount, 1)
	testutil.AssertEqual(t, state[atomFeedURL].LastError, "want 200, got 418: I'm a teapot.\n")
}

func TestDisablingAndReenablingFailingFeed(t *testing.T) {
	t.Parallel()

	tm := testMux(t, txtarToFS(txtar.Parse(defaultGistTxtar)), map[string]http.HandlerFunc{
		atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "I'm a teapot.", http.StatusTeapot)
		},
	})

	f := testFetcher(t, tm)

	const attempts = errorThreshold
	for range attempts {
		if err := f.run(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	state1 := tm.state(t)

	testutil.AssertEqual(t, state1[atomFeedURL].Disabled, true)
	testutil.AssertEqual(t, state1[atomFeedURL].ErrorCount, attempts)
	testutil.AssertEqual(t, state1[atomFeedURL].LastError, "want 200, got 418: I'm a teapot.\n")

	testutil.AssertEqual(t, len(tm.sentMessages), 1)

	if err := f.reenable(t.Context(), atomFeedURL); err != nil {
		t.Fatal(err)
	}
	state2 := tm.state(t)
	testutil.AssertEqual(t, state2[atomFeedURL].Disabled, false)
	testutil.AssertEqual(t, state2[atomFeedURL].ErrorCount, 0)
	testutil.AssertEqual(t, state2[atomFeedURL].LastError, "")
}

func TestFetchWithIfModifiedSinceAndETag(t *testing.T) {
	t.Parallel()

	const (
		ifModifiedSince = "Tue, 25 Jun 2024 12:00:00 GMT"
		eTag            = "test"
	)

	tm := testMux(t, txtarToFS(txtar.Parse(defaultGistTxtar)), map[string]http.HandlerFunc{
		atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("If-Modified-Since") == ifModifiedSince && r.Header.Get("If-None-Match") == eTag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("Last-Modified", ifModifiedSince)
			w.Header().Set("ETag", eTag)
			w.Write(atomFeed)
		},
	})
	f := testFetcher(t, tm)

	// Initial fetch, should update state with Last-Modified and ETag.
	if err := f.run(t.Context()); err != nil {
		t.Fatal(err)
	}

	state1 := tm.state(t)

	testutil.AssertEqual(t, state1[atomFeedURL].LastModified, ifModifiedSince)
	testutil.AssertEqual(t, state1[atomFeedURL].ETag, eTag)
	f.stats.ReadAccess(func(s *stats) {
		testutil.AssertEqual(t, s.NotModifiedFeeds, 0)
	})

	// Second fetch, should use If-Modified-Since and ETag and get 304.
	if err := f.run(t.Context()); err != nil {
		t.Fatal(err)
	}

	state2 := tm.state(t)

	testutil.AssertEqual(t, state2[atomFeedURL].LastModified, ifModifiedSince)
	testutil.AssertEqual(t, state2[atomFeedURL].ETag, eTag)
	f.stats.ReadAccess(func(s *stats) {
		testutil.AssertEqual(t, s.NotModifiedFeeds, 1)
	})
}

//go:embed testdata/feeds/atom_rules.xml
var rulesAtomFeed []byte

func TestBlockAndKeepRules(t *testing.T) {
	t.Parallel()

	testutil.RunGolden(t, "testdata/rules/*.star", func(t *testing.T, match string) []byte {
		t.Parallel()

		config := readFile(t, match)

		state := map[string]*feedState{
			"https://example.com/feed.xml": {
				LastUpdated: time.Time{},
			},
		}
		ar := &txtar.Archive{
			Files: []txtar.File{
				{Name: "config.star", Data: config},
				{Name: "state.json", Data: toJSON(t, state)},
			},
		}

		tm := testMux(t, txtarToFS(ar), map[string]http.HandlerFunc{
			atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(rulesAtomFeed))
			},
		})

		f := testFetcher(t, tm)
		if err := f.run(t.Context()); err != nil {
			t.Fatal(err)
		}

		sort.SliceStable(tm.sentMessages, func(i, j int) bool {
			return compareMaps(tm.sentMessages[i], tm.sentMessages[j])
		})
		return toJSON(t, tm.sentMessages)
	}, *updateGolden)
}

func TestDigestAndFormat(t *testing.T) {
	t.Parallel()

	testutil.RunGolden(t, "testdata/digest/*.star", func(t *testing.T, match string) []byte {
		t.Parallel()

		config := readFile(t, match)

		// Create a mock state where the feed is new (LastUpdated zero).
		state := map[string]*feedState{
			"https://example.com/feed.xml": {
				LastUpdated: time.Time{},
			},
		}
		ar := &txtar.Archive{
			Files: []txtar.File{
				{Name: "config.star", Data: config},
				{Name: "state.json", Data: toJSON(t, state)},
			},
		}

		tm := testMux(t, txtarToFS(ar), map[string]http.HandlerFunc{
			atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
				w.Write(atomFeed)
			},
		})

		f := testFetcher(t, tm)
		if err := f.run(t.Context()); err != nil {
			t.Fatal(err)
		}

		// Sort messages to be deterministic.
		sort.SliceStable(tm.sentMessages, func(i, j int) bool {
			return compareMaps(tm.sentMessages[i], tm.sentMessages[j])
		})
		return toJSON(t, tm.sentMessages)

	}, *updateGolden)
}

func TestSplitMessage(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		want []string
	}{
		"short": {
			in:   "hello",
			want: []string{"hello"},
		},
		"exact": {
			in:   strings.Repeat("a", 4096),
			want: []string{strings.Repeat("a", 4096)},
		},
		"long (no newline)": {
			in:   strings.Repeat("a", 4100),
			want: []string{strings.Repeat("a", 4096), "aaaa"},
		},
		"long (newline split)": {
			in:   strings.Repeat("a", 4000) + "\n" + strings.Repeat("b", 100),
			want: []string{strings.Repeat("a", 4000), "\n" + strings.Repeat("b", 100)},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := splitMessage(tc.in)
			testutil.AssertEqual(t, got, tc.want)
		})
	}
}

func TestAlwaysSendNewItems(t *testing.T) {
	t.Parallel()

	recentDate := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	oldDate := time.Now().Add(-20 * 24 * time.Hour).Format(time.RFC3339) // > 14 days
	newRecentDate := time.Now().Add(-2 * 24 * time.Hour).Format(time.RFC3339)

	feedContent1 := fmt.Sprintf(string(readFile(t, "testdata/always_send_new_items/feed1.xml.tmpl")), recentDate, oldDate)
	feedContent2 := fmt.Sprintf(string(readFile(t, "testdata/always_send_new_items/feed2.xml.tmpl")), recentDate, newRecentDate)

	config := readFile(t, "testdata/always_send_new_items/config.star")

	state := map[string]*feedState{}
	ar := &txtar.Archive{
		Files: []txtar.File{
			{Name: "config.star", Data: config},
			{Name: "state.json", Data: toJSON(t, state)},
		},
	}

	var (
		mu      sync.Mutex
		content string
	)
	content = feedContent1

	tm := testMux(t, txtarToFS(ar), map[string]http.HandlerFunc{
		atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			w.Write([]byte(content))
		},
	})

	f := testFetcher(t, tm)
	if err := f.run(t.Context()); err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, len(tm.sentMessages), 0)

	s := tm.state(t)[atomFeedURL]
	if _, ok := s.SeenItems["item1"]; !ok {
		t.Errorf("item1 should be in SeenItems")
	}
	if _, ok := s.SeenItems["item2"]; ok {
		t.Errorf("item2 should NOT be in SeenItems (too old)")
	}

	// Now add a new item with an old date (but within lookback).
	mu.Lock()
	content = feedContent2
	mu.Unlock()

	if err := f.run(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Should have sent item3.
	testutil.AssertEqual(t, len(tm.sentMessages), 1)
	got := tm.sentMessages[0]["text"].(string)
	if !strings.Contains(got, "New item with old date") {
		t.Errorf("sent message should contain item title, got: %q", got)
	}
	if !strings.Contains(got, "#examplecom") {
		t.Errorf("sent message should contain hashtag, got: %q", got)
	}

	// Third run, same feed, should NOT send anything again.
	if err := f.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, len(tm.sentMessages), 1)
}

func TestParseTGICASURetryIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		body  string
		want  time.Duration
		found bool
	}{
		{
			name:  "flood wait",
			body:  `{"errors":["FLOOD_WAIT_42"]}`,
			want:  42 * time.Second,
			found: true,
		},
		{
			name:  "unlock access",
			body:  `{"errors":["Time to unlock access: 01:02:03"]}`,
			want:  1*time.Hour + 2*time.Minute + 3*time.Second,
			found: true,
		},
		{
			name:  "mixed errors picks first valid",
			body:  `{"errors":[123,"oops","FLOOD_WAIT_5"]}`,
			want:  5 * time.Second,
			found: true,
		},
		{
			name:  "unknown format",
			body:  `{"errors":["something else"]}`,
			want:  0,
			found: false,
		},
		{
			name:  "invalid json",
			body:  `{`,
			want:  0,
			found: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, found := parseTGICASURetryIn([]byte(tc.body))
			testutil.AssertEqual(t, found, tc.found)
			testutil.AssertEqual(t, got, tc.want)
		})
	}
}

func TestShouldProcessFeedItem(t *testing.T) {
	t.Parallel()

	now := time.Now()
	recent := now.Add(-1 * time.Hour)
	old := now.Add(-15 * 24 * time.Hour)

	tests := []struct {
		name        string
		fd          *feed
		state       *feedState
		item        *gofeed.Item
		exists      bool
		justEnabled bool
		want        bool
		wantSeen    bool
	}{
		{
			name: "always_send_new_items skips old entries",
			fd: &feed{
				alwaysSendNewItems: true,
			},
			state: &feedState{SeenItems: map[string]time.Time{}},
			item: &gofeed.Item{
				GUID:            "old",
				Link:            "https://example.com/old",
				PublishedParsed: &old,
			},
			exists:      true,
			justEnabled: false,
			want:        false,
			wantSeen:    false,
		},
		{
			name: "always_send_new_items includes new entry for existing feed",
			fd: &feed{
				alwaysSendNewItems: true,
			},
			state: &feedState{SeenItems: map[string]time.Time{}},
			item: &gofeed.Item{
				GUID:            "new",
				Link:            "https://example.com/new",
				PublishedParsed: &recent,
			},
			exists:      true,
			justEnabled: false,
			want:        true,
			wantSeen:    true,
		},
		{
			name: "always_send_new_items suppresses first run",
			fd: &feed{
				alwaysSendNewItems: true,
			},
			state: &feedState{SeenItems: map[string]time.Time{}},
			item: &gofeed.Item{
				GUID:            "first",
				Link:            "https://example.com/first",
				PublishedParsed: &recent,
			},
			exists:      false,
			justEnabled: false,
			want:        false,
			wantSeen:    true,
		},
		{
			name: "always_send_new_items skips already seen",
			fd: &feed{
				alwaysSendNewItems: true,
			},
			state: &feedState{SeenItems: map[string]time.Time{
				"seen": now,
			}},
			item: &gofeed.Item{
				GUID:            "seen",
				Link:            "https://example.com/seen",
				PublishedParsed: &recent,
			},
			exists:      true,
			justEnabled: false,
			want:        false,
			wantSeen:    true,
		},
		{
			name: "published before last update is ignored in regular mode",
			fd:   &feed{},
			state: &feedState{
				LastUpdated: now,
			},
			item: &gofeed.Item{
				GUID:            "regular-old",
				Link:            "https://example.com/regular-old",
				PublishedParsed: &recent,
			},
			exists:      true,
			justEnabled: false,
			want:        false,
			wantSeen:    false,
		},
		{
			name: "nil published timestamp is accepted in regular mode",
			fd:   &feed{},
			state: &feedState{
				LastUpdated: now,
			},
			item: &gofeed.Item{
				GUID: "regular-nil",
				Link: "https://example.com/regular-nil",
			},
			exists:      true,
			justEnabled: false,
			want:        true,
			wantSeen:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldProcessFeedItem(tc.fd, tc.state, tc.item, tc.exists, tc.justEnabled)
			testutil.AssertEqual(t, got, tc.want)

			guid := tc.item.GUID
			if guid == "" {
				guid = tc.item.Link
			}
			_, hasSeen := tc.state.SeenItems[guid]
			testutil.AssertEqual(t, hasSeen, tc.wantSeen)
		})
	}
}

func TestHandleFeedStatus(t *testing.T) {
	t.Parallel()

	f := testFetcher(t, testMux(t, nil, nil))
	f.stats = syncx.Protect(&stats{})
	fd := &feed{url: "https://example.com/feed.xml"}

	t.Run("not modified", func(t *testing.T) {
		state := &feedState{
			ErrorCount: 3,
			LastError:  "oops",
		}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, fd.url, nil)
		if err != nil {
			t.Fatal(err)
		}
		res := &http.Response{
			StatusCode: http.StatusNotModified,
			Body:       http.NoBody,
			Header:     make(http.Header),
		}

		result, err := f.handleFeedStatus(req, res, fd, state)
		testutil.AssertEqual(t, err, nil)
		testutil.AssertEqual(t, result.handled, true)
		testutil.AssertEqual(t, result.retry, false)
		testutil.AssertEqual(t, result.retryIn, time.Duration(0))
		testutil.AssertEqual(t, state.ErrorCount, 0)
		testutil.AssertEqual(t, state.LastError, "")
		if state.LastUpdated.IsZero() {
			t.Fatal("LastUpdated should be set")
		}
		f.stats.ReadAccess(func(s *stats) {
			testutil.AssertEqual(t, s.NotModifiedFeeds, 1)
		})
	})

	t.Run("200 status", func(t *testing.T) {
		state := &feedState{}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, fd.url, nil)
		if err != nil {
			t.Fatal(err)
		}
		res := &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
			Header:     make(http.Header),
		}

		result, err := f.handleFeedStatus(req, res, fd, state)
		testutil.AssertEqual(t, err, nil)
		testutil.AssertEqual(t, result.handled, false)
		testutil.AssertEqual(t, result.retry, false)
		testutil.AssertEqual(t, result.retryIn, time.Duration(0))
	})

	t.Run("tg.i-c-a.su retry", func(t *testing.T) {
		state := &feedState{}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://tg.i-c-a.su/feed.json", nil)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		rec.WriteHeader(http.StatusTooManyRequests)
		rec.WriteString(`{"errors":["FLOOD_WAIT_15"]}`)

		result, err := f.handleFeedStatus(req, rec.Result(), fd, state)
		testutil.AssertEqual(t, err, nil)
		testutil.AssertEqual(t, result.handled, true)
		testutil.AssertEqual(t, result.retry, true)
		testutil.AssertEqual(t, result.retryIn, 15*time.Second)
	})

	t.Run("non-200 returns error", func(t *testing.T) {
		state := &feedState{}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, fd.url, nil)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		rec.WriteHeader(http.StatusTeapot)
		rec.WriteString("teapot")

		result, err := f.handleFeedStatus(req, rec.Result(), fd, state)
		if err == nil {
			t.Fatal("expected error")
		}
		testutil.AssertEqual(t, result.handled, true)
		testutil.AssertEqual(t, result.retry, false)
		testutil.AssertEqual(t, result.retryIn, time.Duration(0))
		if !strings.Contains(err.Error(), "want 200, got 418: teapot") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestParseFormattedMessage(t *testing.T) {
	t.Parallel()

	t.Run("string", func(t *testing.T) {
		msg, replyMarkup, ok := parseFormattedMessage(starlark.String("hello"))
		testutil.AssertEqual(t, ok, true)
		testutil.AssertEqual(t, msg, "hello")
		testutil.AssertEqual(t, replyMarkup, (*inlineKeyboard)(nil))
	})

	t.Run("tuple with keyboard", func(t *testing.T) {
		keyboard := starlark.NewList([]starlark.Value{
			starlark.NewList([]starlark.Value{
				starlark.NewDict(2),
			}),
		})
		dict := keyboard.Index(0).(*starlark.List).Index(0).(*starlark.Dict)
		dict.SetKey(starlark.String("text"), starlark.String("Open"))
		dict.SetKey(starlark.String("url"), starlark.String("https://example.com"))

		msg, replyMarkup, ok := parseFormattedMessage(starlark.Tuple{starlark.String("formatted"), keyboard})
		testutil.AssertEqual(t, ok, true)
		testutil.AssertEqual(t, msg, "formatted")
		if replyMarkup == nil {
			t.Fatal("replyMarkup should not be nil")
		}
		testutil.AssertEqual(t, len(*replyMarkup), 1)
		testutil.AssertEqual(t, len((*replyMarkup)[0]), 1)
		testutil.AssertEqual(t, (*replyMarkup)[0][0].Text, "Open")
		testutil.AssertEqual(t, (*replyMarkup)[0][0].URL, "https://example.com")
	})

	t.Run("unsupported", func(t *testing.T) {
		msg, replyMarkup, ok := parseFormattedMessage(starlark.MakeInt(1))
		testutil.AssertEqual(t, ok, false)
		testutil.AssertEqual(t, msg, "")
		testutil.AssertEqual(t, replyMarkup, (*inlineKeyboard)(nil))
	})
}

func TestBuildFormatInput(t *testing.T) {
	t.Parallel()

	f := &fetcher{}

	t.Run("single item uses fallback title", func(t *testing.T) {
		u := &update{
			feed:  &feed{},
			items: []*gofeed.Item{{Title: "", Link: "https://example.com/a"}},
		}

		_, gotTitle := f.buildFormatInput(u)
		testutil.AssertEqual(t, gotTitle, "https://example.com/a")
	})

	t.Run("digest uses feed URL when title is empty", func(t *testing.T) {
		u := &update{
			feed:  &feed{digest: true, url: "https://example.com/feed.xml"},
			items: []*gofeed.Item{{Title: "Item", Link: "https://example.com/a"}},
		}

		_, gotTitle := f.buildFormatInput(u)
		testutil.AssertEqual(t, gotTitle, "Updates from https://example.com/feed.xml")
	})
}

func TestDefaultUpdateMessage(t *testing.T) {
	t.Parallel()

	t.Run("adds hacker news keyboard", func(t *testing.T) {
		u := &update{feed: &feed{}, items: []*gofeed.Item{{
			Title: "Title",
			Link:  "https://example.com/post",
			GUID:  "https://news.ycombinator.com/item?id=1",
		}}}

		_, replyMarkup := defaultUpdateMessage(u, "Title")
		if replyMarkup == nil {
			t.Fatal("replyMarkup should not be nil")
		}
		testutil.AssertEqual(t, (*replyMarkup)[0][0].Text, "↪ Hacker News")
	})
}

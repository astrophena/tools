// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
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
	"go.astrophena.name/tools/cmd/tgfeed/internal/sender"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

func TestFailingFeed(t *testing.T) {
	t.Parallel()

	tm := testMux(t, txtarToFS(txtar.Parse(defaultTxtar)), map[string]http.HandlerFunc{
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

	tm := testMux(t, txtarToFS(txtar.Parse(defaultTxtar)), map[string]http.HandlerFunc{
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

	tm := testMux(t, txtarToFS(txtar.Parse(defaultTxtar)), map[string]http.HandlerFunc{
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

func TestAlwaysSendNewItems(t *testing.T) {
	t.Parallel()

	recentDate := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	oldDate := time.Now().Add(-20 * 24 * time.Hour).Format(time.RFC3339) // > 14 days
	newRecentDate := time.Now().Add(-2 * 24 * time.Hour).Format(time.RFC3339)

	feedContent1 := fmt.Sprintf(string(readFile(t, "testdata/new_items/feed1.xml.tmpl")), recentDate, oldDate)
	feedContent2 := fmt.Sprintf(string(readFile(t, "testdata/new_items/feed2.xml.tmpl")), recentDate, newRecentDate)

	config := readFile(t, "testdata/new_items/config.star")

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

	cases := map[string]struct {
		body  string
		want  time.Duration
		found bool
	}{
		"flood wait": {
			body:  `{"errors":["FLOOD_WAIT_42"]}`,
			want:  42 * time.Second,
			found: true,
		},
		"unlock access": {
			body:  `{"errors":["Time to unlock access: 01:02:03"]}`,
			want:  1*time.Hour + 2*time.Minute + 3*time.Second,
			found: true,
		},
		"mixed errors picks first valid": {
			body:  `{"errors":[123,"oops","FLOOD_WAIT_5"]}`,
			want:  5 * time.Second,
			found: true,
		},
		"unknown format": {
			body:  `{"errors":["something else"]}`,
			want:  0,
			found: false,
		},
		"invalid json": {
			body:  `{`,
			want:  0,
			found: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, found := parseTGICASURetryIn([]byte(tc.body))
			testutil.AssertEqual(t, found, tc.found)
			testutil.AssertEqual(t, got, tc.want)
		})
	}
}

func TestDecideFeedItem(t *testing.T) {
	t.Parallel()

	now := time.Now()
	recent := now.Add(-1 * time.Hour)
	old := now.Add(-15 * 24 * time.Hour)

	cases := map[string]struct {
		fd            *feed
		state         *feedState
		item          *gofeed.Item
		exists        bool
		justEnabled   bool
		wantSelection feedItemSelection
		wantMarkSeen  string
	}{
		"always_send_new_items skips old entries": {
			fd: &feed{
				alwaysSendNewItems: true,
			},
			state: &feedState{SeenItems: map[string]time.Time{}},
			item: &gofeed.Item{
				GUID:            "old",
				Link:            "https://example.com/old",
				PublishedParsed: &old,
			},
			exists:        true,
			justEnabled:   false,
			wantSelection: feedItemSelectionSkip,
			wantMarkSeen:  "",
		},
		"always_send_new_items includes new entry for existing feed": {
			fd: &feed{
				alwaysSendNewItems: true,
			},
			state: &feedState{SeenItems: map[string]time.Time{}},
			item: &gofeed.Item{
				GUID:            "new",
				Link:            "https://example.com/new",
				PublishedParsed: &recent,
			},
			exists:        true,
			justEnabled:   false,
			wantSelection: feedItemSelectionProcess,
			wantMarkSeen:  "new",
		},
		"always_send_new_items suppresses first run": {
			fd: &feed{
				alwaysSendNewItems: true,
			},
			state: &feedState{SeenItems: map[string]time.Time{}},
			item: &gofeed.Item{
				GUID:            "first",
				Link:            "https://example.com/first",
				PublishedParsed: &recent,
			},
			exists:        false,
			justEnabled:   false,
			wantSelection: feedItemSelectionMarkSeenOnly,
			wantMarkSeen:  "first",
		},
		"always_send_new_items skips already seen": {
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
			exists:        true,
			justEnabled:   false,
			wantSelection: feedItemSelectionSkip,
			wantMarkSeen:  "",
		},
		"published before last update is ignored in regular mode": {
			fd: &feed{},
			state: &feedState{
				LastUpdated: now,
			},
			item: &gofeed.Item{
				GUID:            "regular-old",
				Link:            "https://example.com/regular-old",
				PublishedParsed: &recent,
			},
			exists:        true,
			justEnabled:   false,
			wantSelection: feedItemSelectionSkip,
			wantMarkSeen:  "",
		},
		"nil published timestamp is accepted in regular mode": {
			fd: &feed{},
			state: &feedState{
				LastUpdated: now,
			},
			item: &gofeed.Item{
				GUID: "regular-nil",
				Link: "https://example.com/regular-nil",
			},
			exists:        true,
			justEnabled:   false,
			wantSelection: feedItemSelectionProcess,
			wantMarkSeen:  "",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			beforeSeenCount := len(tc.state.SeenItems)
			itemCtx := feedItemContext{
				feed:        tc.fd,
				state:       tc.state,
				exists:      tc.exists,
				justEnabled: tc.justEnabled,
			}
			decision := decideFeedItem(itemCtx, tc.item)
			testutil.AssertEqual(t, decision.selection, tc.wantSelection)
			testutil.AssertEqual(t, decision.markSeen, tc.wantMarkSeen)
			testutil.AssertEqual(t, len(tc.state.SeenItems), beforeSeenCount)
		})
	}
}

func TestDecideEnqueueAction(t *testing.T) {
	t.Parallel()

	makeRule := func(t *testing.T, src string, name string) *starlark.Function {
		t.Helper()
		globals, err := starlark.ExecFileOptions(&syntax.FileOptions{}, &starlark.Thread{Name: "test"}, "rule.star", src, nil)
		if err != nil {
			t.Fatal(err)
		}
		v, ok := globals[name]
		if !ok {
			t.Fatalf("rule %q not found", name)
		}
		fn, ok := v.(*starlark.Function)
		if !ok {
			t.Fatalf("rule %q is %T, want *starlark.Function", name, v)
		}
		return fn
	}

	cases := map[string]struct {
		fd   *feed
		item *gofeed.Item
		want enqueueAction
	}{
		"single action for non-digest feed": {
			fd:   &feed{digest: false},
			item: &gofeed.Item{Link: "https://example.com/a"},
			want: enqueueActionSingle,
		},
		"digest action for digest feed": {
			fd:   &feed{digest: true},
			item: &gofeed.Item{Link: "https://example.com/a"},
			want: enqueueActionDigest,
		},
		"skip when blocked": {
			fd: &feed{
				digest:    false,
				blockRule: makeRule(t, "def rule(item):\n  return True\n", "rule"),
			},
			item: &gofeed.Item{Link: "https://example.com/a"},
			want: enqueueActionSkip,
		},
		"skip when keep rule rejects": {
			fd: &feed{
				digest:   true,
				keepRule: makeRule(t, "def rule(item):\n  return False\n", "rule"),
			},
			item: &gofeed.Item{Link: "https://example.com/a"},
			want: enqueueActionSkip,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := testFetcher(t, testMux(t, nil, nil))
			got := f.decideEnqueueAction(feedItemContext{feed: tc.fd}, tc.item)
			testutil.AssertEqual(t, got, tc.want)
		})
	}
}

func TestHandleFeedStatus(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		reqURL                 string
		statusCode             int
		body                   string
		initialState           feedState
		wantHandled            bool
		wantRetry              bool
		wantRetryIn            time.Duration
		wantErrContains        string
		wantErrorCount         int
		wantLastError          string
		wantLastUpdatedNonZero bool
		wantNotModifiedFeeds   int
	}{
		"not modified": {
			reqURL:     "https://example.com/feed.xml",
			statusCode: http.StatusNotModified,
			initialState: feedState{
				ErrorCount: 3,
				LastError:  "oops",
			},
			wantHandled:            true,
			wantRetry:              false,
			wantRetryIn:            0,
			wantErrorCount:         0,
			wantLastError:          "",
			wantLastUpdatedNonZero: true,
			wantNotModifiedFeeds:   1,
		},
		"200 status": {
			reqURL:         "https://example.com/feed.xml",
			statusCode:     http.StatusOK,
			wantHandled:    false,
			wantRetry:      false,
			wantRetryIn:    0,
			wantErrorCount: 0,
			wantLastError:  "",
		},
		"tg.i-c-a.su retry": {
			reqURL:         "https://tg.i-c-a.su/feed.json",
			statusCode:     http.StatusTooManyRequests,
			body:           `{"errors":["FLOOD_WAIT_15"]}`,
			wantHandled:    true,
			wantRetry:      true,
			wantRetryIn:    15 * time.Second,
			wantErrorCount: 0,
			wantLastError:  "",
		},
		"non-200 returns error": {
			reqURL:          "https://example.com/feed.xml",
			statusCode:      http.StatusTeapot,
			body:            "teapot",
			wantHandled:     true,
			wantRetry:       false,
			wantRetryIn:     0,
			wantErrContains: "want 200, got 418: teapot",
			wantErrorCount:  0,
			wantLastError:   "",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := testFetcher(t, testMux(t, nil, nil))
			f.stats = syncx.Protect(&stats{})
			fd := &feed{url: "https://example.com/feed.xml"}
			state := tc.initialState
			f.state = syncx.Protect(map[string]*feedState{
				fd.url: &state,
			})

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, tc.reqURL, nil)
			if err != nil {
				t.Fatal(err)
			}

			rec := httptest.NewRecorder()
			rec.WriteHeader(tc.statusCode)
			if tc.body != "" {
				rec.WriteString(tc.body)
			}

			result, err := f.handleFeedStatus(req, rec.Result(), fd)

			if tc.wantErrContains != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				testutil.AssertEqual(t, err, nil)
			}

			testutil.AssertEqual(t, result.handled, tc.wantHandled)
			testutil.AssertEqual(t, result.retry, tc.wantRetry)
			testutil.AssertEqual(t, result.retryIn, tc.wantRetryIn)
			testutil.AssertEqual(t, state.ErrorCount, tc.wantErrorCount)
			testutil.AssertEqual(t, state.LastError, tc.wantLastError)
			testutil.AssertEqual(t, !state.LastUpdated.IsZero(), tc.wantLastUpdatedNonZero)

			f.stats.ReadAccess(func(s *stats) {
				testutil.AssertEqual(t, s.NotModifiedFeeds, tc.wantNotModifiedFeeds)
			})
		})
	}
}

type captureSender struct {
	messages []sender.Message
}

func (s *captureSender) Send(_ context.Context, msg sender.Message) error {
	s.messages = append(s.messages, msg)
	return nil
}

func TestSendUpdateUsesInjectedSender(t *testing.T) {
	t.Parallel()

	f := &fetcher{slog: slog.Default()}
	mock := &captureSender{}
	f.sender = mock

	u := &update{
		feed:  &feed{url: "https://example.com/feed.xml", messageThreadID: 7},
		items: []*gofeed.Item{{Title: "hello", Link: "https://example.com/a"}},
	}

	f.sendUpdate(t.Context(), u)
	testutil.AssertEqual(t, len(mock.messages), 1)
	testutil.AssertEqual(t, mock.messages[0].Target.Topic, "7")
	if !strings.Contains(mock.messages[0].Body, "hello") {
		t.Fatalf("sent body %q does not include title", mock.messages[0].Body)
	}
}

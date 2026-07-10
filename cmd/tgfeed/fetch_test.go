// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mmcdole/gofeed"
	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/syncx"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/tools/cmd/tgfeed/internal/sender"
	"go.astrophena.name/tools/cmd/tgfeed/internal/state"
	tgstats "go.astrophena.name/tools/cmd/tgfeed/internal/stats"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

func TestFailingFeed(t *testing.T) {
	t.Parallel()

	env := newDefaultTestEnv(t, map[string]http.HandlerFunc{
		atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "I'm a teapot.", http.StatusTeapot)
		},
	})
	f := newTestFetcher(t, env)
	if err := f.run(t.Context()); err != nil {
		t.Fatal(err)
	}

	state := env.state(t)

	testutil.AssertEqual(t, state[atomFeedURL].ErrorCount, 1)
	testutil.AssertEqual(t, state[atomFeedURL].LastError, "want 200, got 418: I'm a teapot.\n")
}

func TestRetryExhaustionCountsAsFailure(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var attempts atomic.Int32
		env := newDefaultTestEnv(t, map[string]http.HandlerFunc{
			atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			},
		})
		f := newTestFetcher(t, env)
		if err := f.run(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := f.statsStore.Close(); err != nil {
			t.Fatal(err)
		}

		state := env.state(t)

		testutil.AssertEqual(t, attempts.Load(), int32(retryLimit+1))
		testutil.AssertEqual(t, state[atomFeedURL].ErrorCount, 1)
		testutil.AssertEqual(t, state[atomFeedURL].FetchFailCount, int64(1))
		if !strings.Contains(state[atomFeedURL].LastError, fmt.Sprintf("retry limit exceeded after %d retries", retryLimit)) {
			t.Fatalf("unexpected last error: %q", state[atomFeedURL].LastError)
		}

		f.stats.ReadAccess(func(s *tgstats.Run) {
			testutil.AssertEqual(t, s.FailedFeeds, 1)
			testutil.AssertEqual(t, s.SuccessFeeds, 0)
			testutil.AssertEqual(t, s.NotModifiedFeeds, 0)
			testutil.AssertEqual(t, s.FetchRetriesTotal, retryLimit)
			testutil.AssertEqual(t, s.FeedsRetriedCount, 1)
			testutil.AssertEqual(t, s.HTTP5xxCount, retryLimit+1)
		})
	})
}

func TestDisablingAndReenablingFailingFeed(t *testing.T) {
	t.Parallel()

	env := newDefaultTestEnv(t, map[string]http.HandlerFunc{
		atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "I'm a teapot.", http.StatusTeapot)
		},
	})

	f := newTestFetcher(t, env)

	const attempts = errorThreshold
	for range attempts {
		if err := f.run(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	state1 := env.state(t)

	testutil.AssertEqual(t, state1[atomFeedURL].Disabled, true)
	testutil.AssertEqual(t, state1[atomFeedURL].ErrorCount, attempts)
	testutil.AssertEqual(t, state1[atomFeedURL].LastError, "want 200, got 418: I'm a teapot.\n")

	testutil.AssertEqual(t, len(env.sentMessages), 1)

	if err := f.reenable(t.Context(), atomFeedURL); err != nil {
		t.Fatal(err)
	}
	state2 := env.state(t)
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

	env := newDefaultTestEnv(t, map[string]http.HandlerFunc{
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
	f := newTestFetcher(t, env)

	// Initial fetch, should update state with Last-Modified and ETag.
	if err := f.run(t.Context()); err != nil {
		t.Fatal(err)
	}

	state1 := env.state(t)

	testutil.AssertEqual(t, state1[atomFeedURL].LastModified, ifModifiedSince)
	testutil.AssertEqual(t, state1[atomFeedURL].ETag, eTag)
	f.stats.ReadAccess(func(s *tgstats.Run) {
		testutil.AssertEqual(t, s.NotModifiedFeeds, 0)
	})

	// Second fetch, should use If-Modified-Since and ETag and get 304.
	if err := f.run(t.Context()); err != nil {
		t.Fatal(err)
	}

	state2 := env.state(t)

	testutil.AssertEqual(t, state2[atomFeedURL].LastModified, ifModifiedSince)
	testutil.AssertEqual(t, state2[atomFeedURL].ETag, eTag)
	f.stats.ReadAccess(func(s *tgstats.Run) {
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

		state := map[string]*state.Feed{
			"https://example.com/feed.xml": {
				LastUpdated: time.Time{},
			},
		}
		env := newTestEnv(t, stateArchive(t, config, state), map[string]http.HandlerFunc{
			atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(rulesAtomFeed))
			},
		})

		f := newTestFetcher(t, env)
		if err := f.run(t.Context()); err != nil {
			if filepath.Base(match) == "invalid.star" {
				return []byte("rule error\n")
			}
			t.Fatal(err)
		}

		return env.sortedSentMessagesJSON(t)
	}, *updateGolden)
}

func TestDigestAndFormat(t *testing.T) {
	t.Parallel()

	testutil.RunGolden(t, "testdata/digest/*.star", func(t *testing.T, match string) []byte {
		t.Parallel()

		config := readFile(t, match)

		// Create a mock state where the feed is new (LastUpdated zero).
		state := map[string]*state.Feed{
			"https://example.com/feed.xml": {
				LastUpdated: time.Time{},
			},
		}
		env := newTestEnv(t, stateArchive(t, config, state), map[string]http.HandlerFunc{
			atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
				w.Write(atomFeed)
			},
		})

		f := newTestFetcher(t, env)
		if err := f.run(t.Context()); err != nil {
			t.Fatal(err)
		}

		return env.sortedSentMessagesJSON(t)

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

	state := map[string]*state.Feed{}
	var (
		mu      sync.Mutex
		content string
	)
	content = feedContent1

	env := newTestEnv(t, stateArchive(t, config, state), map[string]http.HandlerFunc{
		atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			w.Write([]byte(content))
		},
	})

	f := newTestFetcher(t, env)
	if err := f.run(t.Context()); err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, len(env.sentMessages), 0)

	s := env.state(t)[atomFeedURL]
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
	testutil.AssertEqual(t, len(env.sentMessages), 1)
	got := env.sentText(t, 0)
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
	testutil.AssertEqual(t, len(env.sentMessages), 1)
}

func TestAlwaysSendNewItemsUsesPublishedDateForLookback(t *testing.T) {
	t.Parallel()

	oldPublished := time.Now().Add(-20 * 24 * time.Hour).Format(time.RFC3339)
	recentUpdated := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	config := readFile(t, "testdata/new_items/config.star")

	state := map[string]*state.Feed{
		atomFeedURL: {
			SeenItems: map[string]time.Time{},
		},
	}
	const feedTemplate = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Test Feed</title>
  <entry>
    <title>Old published, recently updated</title>
    <link href="http://example.com/item-old"/>
    <id>item-old</id>
    <published>%s</published>
    <updated>%s</updated>
  </entry>
</feed>`

	env := newTestEnv(t, stateArchive(t, config, state), map[string]http.HandlerFunc{
		atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, feedTemplate, oldPublished, recentUpdated)
		},
	})

	f := newTestFetcher(t, env)
	if err := f.run(t.Context()); err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, len(env.sentMessages), 0)
	if _, ok := env.state(t)[atomFeedURL].SeenItems["item-old"]; ok {
		t.Fatalf("old published item should not be tracked as seen")
	}
}

func TestDecideFeedItem(t *testing.T) {
	t.Parallel()

	now := time.Now()
	recent := now.Add(-1 * time.Hour)
	old := now.Add(-15 * 24 * time.Hour)

	cases := map[string]struct {
		fd                   *feed
		state                *state.Feed
		item                 *gofeed.Item
		exists               bool
		seenItemsInitialized bool
		wantProcess          bool
		wantMarkSeen         string
		wantSkip             tgstats.FeedItemDecisionReason
	}{
		"always_send_new_items skips old entries": {
			fd: &feed{
				alwaysSendNewItems: true,
			},
			state: &state.Feed{SeenItems: map[string]time.Time{}},
			item: &gofeed.Item{
				GUID:            "old",
				Link:            "https://example.com/old",
				PublishedParsed: &old,
			},
			exists:       true,
			wantProcess:  false,
			wantMarkSeen: "",
			wantSkip:     tgstats.FeedItemSkipReasonOld,
		},
		"always_send_new_items prefers published over updated for lookback": {
			fd: &feed{
				alwaysSendNewItems: true,
			},
			state: &state.Feed{SeenItems: map[string]time.Time{}},
			item: &gofeed.Item{
				GUID:            "published-old",
				Link:            "https://example.com/published-old",
				PublishedParsed: &old,
				UpdatedParsed:   &recent,
			},
			exists:       true,
			wantProcess:  false,
			wantMarkSeen: "",
			wantSkip:     tgstats.FeedItemSkipReasonOld,
		},
		"always_send_new_items falls back to updated when published is missing": {
			fd: &feed{
				alwaysSendNewItems: true,
			},
			state: &state.Feed{SeenItems: map[string]time.Time{}},
			item: &gofeed.Item{
				GUID:          "updated-only",
				Link:          "https://example.com/updated-only",
				UpdatedParsed: &recent,
			},
			exists:       true,
			wantProcess:  true,
			wantMarkSeen: "updated-only",
		},
		"always_send_new_items includes new entry for existing feed": {
			fd: &feed{
				alwaysSendNewItems: true,
			},
			state: &state.Feed{SeenItems: map[string]time.Time{}},
			item: &gofeed.Item{
				GUID:            "new",
				Link:            "https://example.com/new",
				PublishedParsed: &recent,
			},
			exists:       true,
			wantProcess:  true,
			wantMarkSeen: "new",
		},
		"always_send_new_items suppresses first run": {
			fd: &feed{
				alwaysSendNewItems: true,
			},
			state: &state.Feed{SeenItems: map[string]time.Time{}},
			item: &gofeed.Item{
				GUID:            "first",
				Link:            "https://example.com/first",
				PublishedParsed: &recent,
			},
			exists:       false,
			wantProcess:  false,
			wantMarkSeen: "first",
			wantSkip:     tgstats.FeedItemSkipReasonUnknown,
		},
		"always_send_new_items suppresses seen-items migration run": {
			fd: &feed{
				alwaysSendNewItems: true,
			},
			state: &state.Feed{SeenItems: map[string]time.Time{}},
			item: &gofeed.Item{
				GUID:            "migration",
				Link:            "https://example.com/migration",
				PublishedParsed: &recent,
			},
			exists:               true,
			seenItemsInitialized: true,
			wantProcess:          false,
			wantMarkSeen:         "migration",
			wantSkip:             tgstats.FeedItemSkipReasonUnknown,
		},
		"always_send_new_items skips already seen": {
			fd: &feed{
				alwaysSendNewItems: true,
			},
			state: &state.Feed{SeenItems: map[string]time.Time{
				"seen": now,
			}},
			item: &gofeed.Item{
				GUID:            "seen",
				Link:            "https://example.com/seen",
				PublishedParsed: &recent,
			},
			exists:       true,
			wantProcess:  false,
			wantMarkSeen: "",
			wantSkip:     tgstats.FeedItemSkipReasonSeen,
		},
		"regular mode ignores recently updated item with old published date": {
			fd: &feed{},
			state: &state.Feed{
				LastUpdated: now,
			},
			item: &gofeed.Item{
				GUID:            "regular-old-updated",
				Link:            "https://example.com/regular-old-updated",
				PublishedParsed: &old,
				UpdatedParsed:   &recent,
			},
			exists:       true,
			wantProcess:  false,
			wantMarkSeen: "",
			wantSkip:     tgstats.FeedItemSkipReasonOld,
		},
		"regular mode falls back to updated when published is missing": {
			fd: &feed{},
			state: &state.Feed{
				LastUpdated: old,
			},
			item: &gofeed.Item{
				GUID:          "regular-updated-only",
				Link:          "https://example.com/regular-updated-only",
				UpdatedParsed: &recent,
			},
			exists:       true,
			wantProcess:  true,
			wantMarkSeen: "regular-updated-only",
		},
		"nil published timestamp is accepted in regular mode": {
			fd: &feed{},
			state: &state.Feed{
				LastUpdated: now,
			},
			item: &gofeed.Item{
				GUID: "regular-nil",
				Link: "https://example.com/regular-nil",
			},
			exists:       true,
			wantProcess:  true,
			wantMarkSeen: "regular-nil",
		},
		"missing item identity is skipped": {
			fd:    &feed{},
			state: &state.Feed{SeenItems: map[string]time.Time{}},
			item: &gofeed.Item{
				Title:           "no stable identity",
				PublishedParsed: &recent,
			},
			exists:       true,
			wantProcess:  false,
			wantMarkSeen: "",
			wantSkip:     tgstats.FeedItemSkipReasonUnknown,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			beforeSeenCount := len(tc.state.SeenItems)
			itemCtx := feedItemContext{
				feed:                 tc.fd,
				state:                tc.state,
				exists:               tc.exists,
				seenItemsInitialized: tc.seenItemsInitialized,
			}
			decision := decideFeedItem(now, itemCtx, tc.item)
			testutil.AssertEqual(t, decision.process, tc.wantProcess)
			testutil.AssertEqual(t, decision.markSeen, tc.wantMarkSeen)
			testutil.AssertEqual(t, decision.skipReason, tc.wantSkip)
			testutil.AssertEqual(t, len(tc.state.SeenItems), beforeSeenCount)
		})
	}
}

func TestFeedItemPassesRules(t *testing.T) {
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
		want bool
	}{
		"passes without rules": {
			fd:   &feed{digest: false},
			item: &gofeed.Item{Link: "https://example.com/a"},
			want: true,
		},
		"skip when blocked": {
			fd: &feed{
				digest:    false,
				blockRule: makeRule(t, "def rule(item):\n  return True\n", "rule"),
			},
			item: &gofeed.Item{Link: "https://example.com/a"},
			want: false,
		},
		"skip when keep rule rejects": {
			fd: &feed{
				digest:   true,
				keepRule: makeRule(t, "def rule(item):\n  return False\n", "rule"),
			},
			item: &gofeed.Item{Link: "https://example.com/a"},
			want: false,
		},
		"return rule error": {
			fd: &feed{
				blockRule: makeRule(t, "def rule(item):\n  return 1 / 0\n", "rule"),
			},
			item: &gofeed.Item{Link: "https://example.com/a"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newTestFetcher(t, newTestEnv(t, nil, nil))
			got, err := f.feedItemPassesRules(tc.fd, tc.item, f.itemToStarlark(tc.item))
			if name == "return rule error" {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, got, tc.want)
		})
	}
}

func TestHandleFeedStatus(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		reqURL               string
		statusCode           int
		body                 string
		initialState         state.Feed
		wantNotModified      bool
		wantRetryIn          time.Duration
		wantErrContains      string
		wantNotModifiedFeeds int
	}{
		"not modified": {
			reqURL:     "https://example.com/feed.xml",
			statusCode: http.StatusNotModified,
			initialState: state.Feed{
				ErrorCount: 3,
				LastError:  "oops",
			},
			wantNotModified:      true,
			wantRetryIn:          0,
			wantNotModifiedFeeds: 1,
		},
		"200 status": {
			reqURL:          "https://example.com/feed.xml",
			statusCode:      http.StatusOK,
			wantNotModified: false,
			wantRetryIn:     0,
		},
		"tg.i-c-a.su retry": {
			reqURL:          "https://tg.i-c-a.su/feed.json",
			statusCode:      http.StatusTooManyRequests,
			body:            `{"errors":["FLOOD_WAIT_15"]}`,
			wantNotModified: false,
			wantRetryIn:     15 * time.Second,
		},
		"non-200 returns error": {
			reqURL:          "https://example.com/feed.xml",
			statusCode:      http.StatusTeapot,
			body:            "teapot",
			wantNotModified: false,
			wantRetryIn:     0,
			wantErrContains: "want 200, got 418: teapot",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newTestFetcher(t, newTestEnv(t, nil, nil))
			f.stats = syncx.Protect(&tgstats.Run{})
			fd := &feed{url: "https://example.com/feed.xml"}
			st := tc.initialState
			f.state = map[string]*state.Feed{
				fd.url: &st,
			}

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

			testutil.AssertEqual(t, result.notModified, tc.wantNotModified)
			testutil.AssertEqual(t, result.retryIn, tc.wantRetryIn)
			testutil.AssertEqual(t, st, tc.initialState)

			f.stats.ReadAccess(func(s *tgstats.Run) {
				testutil.AssertEqual(t, s.NotModifiedFeeds, tc.wantNotModifiedFeeds)
			})
		})
	}
}

type captureSender struct {
	mu       sync.Mutex
	err      error
	messages []sender.Message
	ctxErrs  []error
}

func (s *captureSender) Send(ctx context.Context, msg sender.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctxErrs = append(s.ctxErrs, ctx.Err())
	s.messages = append(s.messages, msg)
	return s.err
}

func TestRunCommandPreservesDeliveryError(t *testing.T) {
	t.Parallel()

	env := newDefaultTestEnv(t, map[string]http.HandlerFunc{
		atomFeedRoute: func(w http.ResponseWriter, _ *http.Request) {
			w.Write(atomFeed)
		},
	})
	f := newTestFetcher(t, env)
	wantErr := errors.New("delivery failed")
	f.sender = &captureSender{err: wantErr}

	err := f.Run(cli.WithEnv(t.Context(), &cli.Env{
		Args:   []string{"run"},
		Getenv: func(string) string { return "" },
		Stdin:  strings.NewReader(""),
		Stdout: t.Output(),
		Stderr: t.Output(),
	}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("run error = %v, want delivery error", err)
	}
}

func TestSendUpdateUsesInjectedSender(t *testing.T) {
	t.Parallel()

	f := &fetcher{slog: slog.Default()}
	f.stats = syncx.Protect(&tgstats.Run{})
	mock := &captureSender{}
	f.sender = mock

	u := &update{
		feed:  &feed{url: "https://example.com/feed.xml", messageThreadID: 7},
		items: []*gofeed.Item{{Title: "hello", Link: "https://example.com/a"}},
	}

	if err := f.sendUpdate(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, len(mock.messages), 1)
	testutil.AssertEqual(t, mock.messages[0].Target.Thread, "7")
	if !strings.Contains(mock.messages[0].Body, "hello") {
		t.Fatalf("sent body %q does not include title", mock.messages[0].Body)
	}
}

func TestDeliverUpdatesCommitsOnlyAfterSuccess(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		cancelContext bool
		sendErr       error
		ackErr        error
		wantErr       bool
		wantSeen      bool
		wantAck       bool
	}{
		"successful delivery": {
			wantSeen: true,
			wantAck:  true,
		},
		"failed delivery": {
			sendErr: errors.New("send failed"),
			wantErr: true,
		},
		"failed acknowledgment": {
			ackErr:  errors.New("ack failed"),
			wantErr: true,
			wantAck: true,
		},
		"canceled parent context drains delivery": {
			cancelContext: true,
			wantSeen:      true,
			wantAck:       true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			const feedURL = "https://example.com/feed.xml"
			f := &fetcher{
				slog:  slog.Default(),
				state: map[string]*state.Feed{feedURL: state.NewFeed(time.Now())},
			}
			f.stats = syncx.Protect(&tgstats.Run{})
			mock := &captureSender{err: tc.sendErr}
			f.sender = mock
			ackCalls := 0

			ctx := t.Context()
			if tc.cancelContext {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err := f.deliverUpdates(ctx, []*update{{
				feed:       &feed{url: feedURL},
				items:      []*gofeed.Item{{Title: "hello", Link: "https://example.com/a"}},
				dedupeKeys: []string{"item-1"},
				acknowledge: func(context.Context, []string) error {
					ackCalls++
					return tc.ackErr
				},
			}})
			testutil.AssertEqual(t, err != nil, tc.wantErr)
			testutil.AssertEqual(t, ackCalls > 0, tc.wantAck)
			_, seen := f.state[feedURL].SeenItems["item-1"]
			testutil.AssertEqual(t, seen, tc.wantSeen)
			if tc.cancelContext {
				testutil.AssertEqual(t, mock.ctxErrs, []error{nil})
			}
		})
	}
}

func TestRunReturnsDeliveryFailureWithoutCommittingSeenItems(t *testing.T) {
	t.Parallel()

	var failDelivery atomic.Bool
	failDelivery.Store(true)
	stateMap := map[string]*state.Feed{
		atomFeedURL: {LastUpdated: time.Time{}},
	}
	env := newTestEnv(t, stateArchive(t, []byte(`feed(url="https://example.com/feed.xml")`), stateMap), map[string]http.HandlerFunc{
		atomFeedRoute: func(w http.ResponseWriter, _ *http.Request) {
			w.Write(atomFeed)
		},
		sendTelegram: func(w http.ResponseWriter, _ *http.Request) {
			if failDelivery.Load() {
				http.Error(w, "telegram unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte("{}"))
		},
	})
	f := newTestFetcher(t, env)

	err := f.run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "sending update") {
		t.Fatalf("run error = %v, want delivery failure", err)
	}
	failedState := env.state(t)[atomFeedURL]
	if got := len(failedState.SeenItems); got != 0 {
		t.Fatalf("seen items = %d, want 0 after failed delivery", got)
	}
	if got := len(failedState.PendingItems); got == 0 {
		t.Fatal("failed delivery was not persisted as pending")
	}

	failDelivery.Store(false)
	if err := f.run(t.Context()); err != nil {
		t.Fatal(err)
	}
	deliveredState := env.state(t)[atomFeedURL]
	if got := len(deliveredState.PendingItems); got != 0 {
		t.Fatalf("pending items = %d, want 0 after retry succeeds", got)
	}
	if got := len(deliveredState.SeenItems); got == 0 {
		t.Fatal("successful retry did not commit seen items")
	}
}

func TestFormatterFailureDoesNotCommitSeenItem(t *testing.T) {
	t.Parallel()

	globals, err := starlark.ExecFileOptions(&syntax.FileOptions{}, &starlark.Thread{Name: "test"}, "format.star", "def format(item):\n  return 1 / 0\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	formatFn := globals["format"].(*starlark.Function)

	const feedURL = "https://example.com/feed.xml"
	f := &fetcher{
		slog:   slog.Default(),
		state:  map[string]*state.Feed{feedURL: state.NewFeed(time.Now())},
		sender: &captureSender{},
	}
	f.stats = syncx.Protect(&tgstats.Run{})
	err = f.sendUpdate(t.Context(), &update{
		feed:       &feed{url: feedURL, format: formatFn},
		items:      []*gofeed.Item{{Title: "hello", Link: "https://example.com/a"}},
		dedupeKeys: []string{"item-1"},
	})
	if err == nil || !strings.Contains(err.Error(), "formatting update") {
		t.Fatalf("send error = %v, want formatting failure", err)
	}
	if f.state[feedURL].IsSeen("item-1") {
		t.Fatal("formatter failure committed item as seen")
	}
	f.stats.ReadAccess(func(s *tgstats.Run) {
		testutil.AssertEqual(t, s.MessagesFormattingFailed, 1)
	})
}

func TestItemToStarlarkStripsHTML(t *testing.T) {
	t.Parallel()

	f := &fetcher{}
	item := &gofeed.Item{
		Title:       "Test",
		Link:        "https://example.com",
		Description: "<p>This is a <b>description</b>.</p>",
		Content:     `<div>Some <a href="https://go.dev">content</a> here.</div>`,
	}

	val := f.itemToStarlark(item)

	hasAttrs, ok := val.(starlark.HasAttrs)
	if !ok {
		t.Fatalf("expected starlark.HasAttrs, got %T", val)
	}

	contentVal, err := hasAttrs.Attr("content")
	if err != nil {
		t.Fatalf("missing content attr: %v", err)
	}
	contentStr, ok := starlark.AsString(contentVal)
	if !ok {
		t.Fatalf("content is not string")
	}

	descVal, err := hasAttrs.Attr("description")
	if err != nil {
		t.Fatalf("missing description attr: %v", err)
	}
	descStr, ok := starlark.AsString(descVal)
	if !ok {
		t.Fatalf("description is not string")
	}

	// bluemonday's StrictPolicy completely removes the HTML tags without
	// altering the inner textual data.
	testutil.AssertEqual(t, contentStr, "Some content here.")
	testutil.AssertEqual(t, descStr, "This is a description.")
}

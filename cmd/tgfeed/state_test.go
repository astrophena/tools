// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mmcdole/gofeed"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.astrophena.name/tools/cmd/tgfeed/internal/state"
	"go.astrophena.name/tools/internal/filelock"
)

func TestLoadState(t *testing.T) {
	t.Parallel()

	baseState := fstest.MapFS{
		"config.star": &fstest.MapFile{
			Data: []byte("feeds = []"),
		},
		"error.tmpl": &fstest.MapFile{
			Data: []byte("test"),
		},
	}

	tm := testMux(t, baseState, nil)
	f := testFetcher(t, tm)

	if err := f.loadState(t.Context()); err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, f.errorTemplate, "test")
}

func TestParseConfig(t *testing.T) {
	testutil.RunGolden(t, "testdata/config/*.star", func(t *testing.T, match string) []byte {
		config := readFile(t, match)

		ar := &txtar.Archive{
			Files: []txtar.File{
				{Name: "config.star", Data: config},
			},
		}

		tm := testMux(t, txtarToFS(ar), nil)
		f := testFetcher(t, tm)
		if err := f.run(t.Context()); err != nil {
			return fmt.Appendf(nil, "Error: %v", err)
		}

		return toJSON(t, f.feeds)
	}, *updateGolden)
}

func TestParseConfigDuplicateFeedURL(t *testing.T) {
	t.Parallel()

	tm := testMux(t, nil, nil)
	f := testFetcher(t, tm)

	err := f.loadConfig(t.Context(), `
feed(url="https://example.com/feed.xml")
feed(url="https://example.com/feed.xml", title="Duplicate")
`)
	if err == nil {
		t.Fatal("want error")
	}
	testutil.AssertEqual(t, err.Error(), `duplicate feed URL "https://example.com/feed.xml"`)
}

func TestParseConfigFormatValidation(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		config       string
		wantContains string
	}{
		"runtime error in formatter call": {
			config: `feed(
	url="https://example.com/feed.xml",
	format=lambda item: "oops" (item.title),
)`,
			wantContains: "invalid call of non-function (string)",
		},
		"invalid formatter output type": {
			config: `feed(
	url="https://example.com/feed.xml",
	format=lambda item: 123,
)`,
			wantContains: `format() for feed "https://example.com/feed.xml" returned invalid output: invalid value type "int"`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tm := testMux(t, nil, nil)
			f := testFetcher(t, tm)

			err := f.loadConfig(t.Context(), tc.config)
			if err == nil {
				t.Fatal("want error")
			}
			if !strings.Contains(err.Error(), tc.wantContains) {
				t.Fatalf("error %q does not contain %q", err, tc.wantContains)
			}
		})
	}
}

func TestFeedStateMarkFetchFailure(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		state            *state.Feed
		threshold        int
		err              error
		wantDisabled     bool
		wantState        bool
		wantErrorCount   int
		wantFetchFailure int64
	}{
		"below threshold does not disable": {
			state:            &state.Feed{},
			threshold:        3,
			err:              errors.New("boom"),
			wantDisabled:     false,
			wantState:        false,
			wantErrorCount:   1,
			wantFetchFailure: 1,
		},
		"reaching threshold disables": {
			state: &state.Feed{
				ErrorCount: 2,
			},
			threshold:        3,
			err:              errors.New("boom"),
			wantDisabled:     true,
			wantState:        true,
			wantErrorCount:   3,
			wantFetchFailure: 1,
		},
		"already disabled does not trigger disable transition again": {
			state: &state.Feed{
				Disabled:   true,
				ErrorCount: 5,
			},
			threshold:        3,
			err:              errors.New("boom"),
			wantDisabled:     false,
			wantState:        true,
			wantErrorCount:   6,
			wantFetchFailure: 1,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gotDisabled := tc.state.MarkFetchFailure(tc.err, tc.threshold)
			testutil.AssertEqual(t, gotDisabled, tc.wantDisabled)
			testutil.AssertEqual(t, tc.state.Disabled, tc.wantState)
			testutil.AssertEqual(t, tc.state.ErrorCount, tc.wantErrorCount)
			testutil.AssertEqual(t, tc.state.FetchFailCount, tc.wantFetchFailure)
			testutil.AssertEqual(t, tc.state.LastError, "boom")
		})
	}
}

func TestFeedStateReenable(t *testing.T) {
	t.Parallel()

	s := &state.Feed{
		Disabled:   true,
		ErrorCount: 8,
		LastError:  "test error",
	}

	s.Reenable()

	testutil.AssertEqual(t, s.Disabled, false)
	testutil.AssertEqual(t, s.ErrorCount, 0)
	testutil.AssertEqual(t, s.LastError, "")
}

func TestFeedStatePrepareSeenItems(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-(seenItemsCleanupPeriod + time.Hour))
	recent := now.Add(-time.Hour)

	cases := map[string]struct {
		state            *state.Feed
		wantJustEnabled  bool
		wantPruned       int
		wantSeenItemKeys []string
	}{
		"initializes nil map": {
			state:            &state.Feed{},
			wantJustEnabled:  true,
			wantPruned:       0,
			wantSeenItemKeys: []string{},
		},
		"keeps recent and removes stale": {
			state: &state.Feed{
				SeenItems: map[string]time.Time{
					"stale":  old,
					"recent": recent,
				},
			},
			wantJustEnabled:  false,
			wantPruned:       1,
			wantSeenItemKeys: []string{"recent"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gotJustEnabled, gotPruned := tc.state.PrepareSeenItems(now, seenItemsCleanupPeriod)
			testutil.AssertEqual(t, gotJustEnabled, tc.wantJustEnabled)
			testutil.AssertEqual(t, gotPruned, tc.wantPruned)

			keys := make([]string, 0, len(tc.state.SeenItems))
			for key := range tc.state.SeenItems {
				keys = append(keys, key)
			}
			testutil.AssertEqual(t, keys, tc.wantSeenItemKeys)
		})
	}
}

func TestFeedStateItemDecisions(t *testing.T) {
	t.Parallel()

	now := time.Now()
	recent := now.Add(-1 * time.Hour)
	old := now.Add(-15 * 24 * time.Hour)

	cases := map[string]struct {
		state         *state.Feed
		item          *gofeed.Item
		exists        bool
		justEnabled   bool
		alwaysSend    bool
		wantSelection feedItemSelection
		wantMarkSeen  string
		wantReason    feedItemSkipReason
	}{
		"always-send skips old entries": {
			state: &state.Feed{
				SeenItems: map[string]time.Time{},
			},
			item: &gofeed.Item{
				GUID:            "old",
				Link:            "https://example.com/old",
				PublishedParsed: &old,
			},
			exists:        true,
			alwaysSend:    true,
			wantSelection: feedItemSelectionSkip,
			wantReason:    feedItemSkipReasonOld,
		},
		"always-send processes unseen item for existing feed": {
			state: &state.Feed{
				SeenItems: map[string]time.Time{},
			},
			item: &gofeed.Item{
				GUID:            "new",
				Link:            "https://example.com/new",
				PublishedParsed: &recent,
			},
			exists:        true,
			alwaysSend:    true,
			wantSelection: feedItemSelectionProcess,
			wantMarkSeen:  "new",
		},
		"always-send marks-only on first run": {
			state: &state.Feed{
				SeenItems: map[string]time.Time{},
			},
			item: &gofeed.Item{
				GUID:            "first",
				Link:            "https://example.com/first",
				PublishedParsed: &recent,
			},
			exists:        false,
			alwaysSend:    true,
			wantSelection: feedItemSelectionMarkSeenOnly,
			wantMarkSeen:  "first",
		},
		"always-send skips already seen": {
			state: &state.Feed{
				SeenItems: map[string]time.Time{
					"seen": now,
				},
			},
			item: &gofeed.Item{
				GUID:            "seen",
				Link:            "https://example.com/seen",
				PublishedParsed: &recent,
			},
			exists:        true,
			alwaysSend:    true,
			wantSelection: feedItemSelectionSkip,
			wantReason:    feedItemSkipReasonSeen,
		},
		"regular mode skips published before last update": {
			state: &state.Feed{
				LastUpdated: now,
			},
			item: &gofeed.Item{
				GUID:            "regular-old",
				Link:            "https://example.com/regular-old",
				PublishedParsed: &recent,
			},
			wantSelection: feedItemSelectionSkip,
			wantReason:    feedItemSkipReasonOld,
		},
		"regular mode accepts nil published timestamp": {
			state: &state.Feed{
				LastUpdated: now,
			},
			item: &gofeed.Item{
				GUID: "regular-nil",
				Link: "https://example.com/regular-nil",
			},
			wantSelection: feedItemSelectionProcess,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			decision := decideFeedItem(feedItemContext{
				feed:        &feed{alwaysSendNewItems: tc.alwaysSend},
				state:       tc.state,
				exists:      tc.exists,
				justEnabled: tc.justEnabled,
			}, tc.item)

			testutil.AssertEqual(t, decision.selection, tc.wantSelection)
			testutil.AssertEqual(t, decision.markSeen, tc.wantMarkSeen)
			testutil.AssertEqual(t, decision.reason, tc.wantReason)
		})
	}
}

func TestWithFeedState(t *testing.T) {
	t.Parallel()

	f := &fetcher{store: state.NewStore(state.Options{StateDir: t.TempDir(), DefaultErrorTemplate: "x"})}
	f.state = state.NewFeedSet(f.store, map[string]*state.Feed{})

	const feedURL = "https://example.com/feed.xml"
	var state1 *state.Feed
	if err := f.withFeedState(t.Context(), feedURL, func(state *state.Feed, exists bool) bool {
		state1 = state
		testutil.AssertEqual(t, exists, false)
		testutil.AssertEqual(t, state1.LastUpdated.IsZero(), false)
		return false
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.withFeedState(t.Context(), feedURL, func(state2 *state.Feed, exists bool) bool {
		testutil.AssertEqual(t, exists, true)
		testutil.AssertEqual(t, state2, state1)
		return false
	}); err != nil {
		t.Fatal(err)
	}
}

func TestStateMapJSON(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		input       map[string]*state.Feed
		jsonInput   []byte
		wantMapSize int
	}{
		"marshal and unmarshal roundtrip": {
			input: map[string]*state.Feed{
				"https://example.com/feed.xml": {
					LastUpdated:  time.Date(2026, time.January, 1, 1, 2, 3, 0, time.UTC),
					LastModified: "Mon, 01 Jan 2026 01:02:03 GMT",
					ETag:         "etag",
					ErrorCount:   2,
					LastError:    "err",
				},
			},
			wantMapSize: 1,
		},
		"unmarshal empty bytes returns empty map": {
			jsonInput:   nil,
			wantMapSize: 0,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var (
				b   []byte
				err error
			)
			if tc.input != nil {
				b, err = state.MarshalStateMap(tc.input)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				b = tc.jsonInput
			}

			raw, err := state.UnmarshalStateMap(b)
			if err != nil {
				t.Fatal(err)
			}
			got := raw
			testutil.AssertEqual(t, len(got), tc.wantMapSize)
			if tc.input != nil {
				testutil.AssertEqual(t, got, tc.input)
			}
		})
	}
}

func TestRunLockerAcquireConflict(t *testing.T) {
	t.Parallel()

	lockPath := filepath.Join(t.TempDir(), ".run.lock")
	firstLock, err := filelock.Acquire(lockPath, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := firstLock.Release(); err != nil {
			t.Fatal(err)
		}
	})

	_, err = filelock.Acquire(lockPath, "")
	if !errors.Is(err, filelock.ErrAlreadyLocked) {
		t.Fatalf("want %v, got %v", filelock.ErrAlreadyLocked, err)
	}
}

func TestRunLockLifecycle(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	f := &fetcher{stateDir: dir}

	if err := f.acquireRunLock(); err != nil {
		t.Fatal(err)
	}

	if f.runLock == nil {
		t.Fatal("run lock file descriptor is nil")
	}
	if !f.isRunLocked() {
		t.Fatal("expected run lock to be held")
	}

	lockPath := filepath.Join(dir, ".run.lock")
	payload, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) == 0 {
		t.Fatal("expected lock payload to be present")
	}

	if err := f.releaseRunLock(); err != nil {
		t.Fatal(err)
	}

	if f.runLock != nil {
		t.Fatal("expected run lock file descriptor to be released")
	}
	if f.isRunLocked() {
		t.Fatal("expected run lock to be released")
	}
}

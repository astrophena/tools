// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"errors"
	"fmt"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mmcdole/gofeed"
	"go.astrophena.name/base/syncx"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
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

func TestFeedStateMarkFetchFailure(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		state            *feedState
		threshold        int
		err              error
		wantDisabled     bool
		wantState        bool
		wantErrorCount   int
		wantFetchFailure int64
	}{
		"below threshold does not disable": {
			state:            &feedState{},
			threshold:        3,
			err:              errors.New("boom"),
			wantDisabled:     false,
			wantState:        false,
			wantErrorCount:   1,
			wantFetchFailure: 1,
		},
		"reaching threshold disables": {
			state: &feedState{
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
			state: &feedState{
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
			gotDisabled := tc.state.markFetchFailure(tc.err, tc.threshold)
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

	s := &feedState{
		Disabled:   true,
		ErrorCount: 8,
		LastError:  "test error",
	}

	s.reenable()

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
		state            *feedState
		wantJustEnabled  bool
		wantSeenItemKeys []string
	}{
		"initializes nil map": {
			state:            &feedState{},
			wantJustEnabled:  true,
			wantSeenItemKeys: []string{},
		},
		"keeps recent and removes stale": {
			state: &feedState{
				SeenItems: map[string]time.Time{
					"stale":  old,
					"recent": recent,
				},
			},
			wantJustEnabled:  false,
			wantSeenItemKeys: []string{"recent"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.state.prepareSeenItems(now)
			testutil.AssertEqual(t, got, tc.wantJustEnabled)

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
		state         *feedState
		item          *gofeed.Item
		exists        bool
		justEnabled   bool
		alwaysSend    bool
		wantSelection feedItemSelection
		wantMarkSeen  string
	}{
		"always-send skips old entries": {
			state: &feedState{
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
		},
		"always-send processes unseen item for existing feed": {
			state: &feedState{
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
			state: &feedState{
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
			state: &feedState{
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
		},
		"regular mode skips published before last update": {
			state: &feedState{
				LastUpdated: now,
			},
			item: &gofeed.Item{
				GUID:            "regular-old",
				Link:            "https://example.com/regular-old",
				PublishedParsed: &recent,
			},
			wantSelection: feedItemSelectionSkip,
		},
		"regular mode accepts nil published timestamp": {
			state: &feedState{
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
			var decision feedItemDecision
			if tc.alwaysSend {
				decision = tc.state.decideAlwaysSendItem(tc.item, now, tc.exists, tc.justEnabled)
			} else {
				decision = tc.state.decideRegularItem(tc.item)
			}

			testutil.AssertEqual(t, decision.selection, tc.wantSelection)
			testutil.AssertEqual(t, decision.markSeen, tc.wantMarkSeen)
		})
	}
}

func TestGetOrCreateState(t *testing.T) {
	t.Parallel()

	f := &fetcher{
		state: syncx.Protect(map[string]*feedState{}),
	}

	state1, exists1 := f.getOrCreateState("https://example.com/feed.xml")
	testutil.AssertEqual(t, exists1, false)
	testutil.AssertEqual(t, state1.LastUpdated.IsZero(), false)

	state2, exists2 := f.getOrCreateState("https://example.com/feed.xml")
	testutil.AssertEqual(t, exists2, true)
	testutil.AssertEqual(t, state2, state1)
}

func TestStateMapJSON(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		input       map[string]*feedState
		jsonInput   []byte
		wantMapSize int
	}{
		"marshal and unmarshal roundtrip": {
			input: map[string]*feedState{
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
				b, err = marshalStateMap(tc.input)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				b = tc.jsonInput
			}

			got, err := unmarshalStateMap(b)
			if err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, len(got), tc.wantMapSize)
			if tc.input != nil {
				testutil.AssertEqual(t, got, tc.input)
			}
		})
	}
}

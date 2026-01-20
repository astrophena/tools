// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	_ "embed"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
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

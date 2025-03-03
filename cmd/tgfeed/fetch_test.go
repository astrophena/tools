// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	_ "embed"
	"html"
	"net/http"
	"sort"
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
)

func TestFailingFeed(t *testing.T) {
	t.Parallel()

	tm := testMux(t, map[string]http.HandlerFunc{
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

	tm := testMux(t, map[string]http.HandlerFunc{
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
	testutil.AssertEqual(t, tm.sentMessages[0]["text"], "❌ Something went wrong:\n<pre><code>"+html.EscapeString("fetching feed \"https://example.com/feed.xml\" failed after 12 previous attempts: want 200, got 418: I'm a teapot.\n; feed was disabled, to reenable it run 'tgfeed -reenable \"https://example.com/feed.xml\"'")+"</code></pre>")

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

	tm := testMux(t, map[string]http.HandlerFunc{
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
	f.stats.Access(func(s *stats) {
		testutil.AssertEqual(t, s.NotModifiedFeeds, 0)
	})

	// Second fetch, should use If-Modified-Since and ETag and get 304.
	if err := f.run(t.Context()); err != nil {
		t.Fatal(err)
	}

	state2 := tm.state(t)

	testutil.AssertEqual(t, state2[atomFeedURL].LastModified, ifModifiedSince)
	testutil.AssertEqual(t, state2[atomFeedURL].ETag, eTag)
	f.stats.Access(func(s *stats) {
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

		tm := testMux(t, map[string]http.HandlerFunc{
			atomFeedRoute: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(rulesAtomFeed))
			},
		})

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
		tm.gist = txtarToGist(t, txtar.Format(ar))

		f := testFetcher(t, tm)
		if err := f.run(t.Context()); err != nil {
			t.Fatal(err)
		}

		sort.SliceStable(tm.sentMessages, func(i, j int) bool {
			return compareMaps(tm.sentMessages[i], tm.sentMessages[j])
		})
		return toJSON(t, tm.sentMessages)
	}, *update)
}

func compareMaps(map1, map2 map[string]any) bool {
	text1, ok1 := map1["text"].(string)
	text2, ok2 := map2["text"].(string)
	if !ok1 {
		if !ok2 {
			// Both don't have text, consider them equal (no change in order).
			return false
		}
		// map1 doesn't have text, map2 does, so map2 comes later.
		return false
	}
	if !ok2 {
		// map1 has text, map2 doesn't, so map1 comes earlier
		return true
	}
	// Compare texts alphabetically.
	return text1 < text2
}

// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
)

func TestPutStats(t *testing.T) {
	stateDir := t.TempDir()
	f := &fetcher{
		logf:     t.Logf,
		stateDir: stateDir,
	}

	s := &stats{
		TotalFeeds:       1,
		SuccessFeeds:     2,
		FailedFeeds:      3,
		NotModifiedFeeds: 4,
		StartTime:        time.Date(2023, time.December, 8, 0, 0, 0, 0, time.UTC),
		Duration:         5 * time.Second,
		TotalItemsParsed: 6,
		TotalFetchTime:   7 * time.Second,
		AvgFetchTime:     8 * time.Second,
		MemoryUsage:      9,
	}

	if err := f.putStats(t.Context(), s); err != nil {
		t.Fatal(err)
	}

	statsDir := filepath.Join(stateDir, "stats")
	entries, err := os.ReadDir(statsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file in stats directory, got %d", len(entries))
	}

	statsFile := filepath.Join(statsDir, entries[0].Name())
	b, err := os.ReadFile(statsFile)
	if err != nil {
		t.Fatal(err)
	}

	var got stats
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, got, *s)
}

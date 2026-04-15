// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
)

func TestPutStats(t *testing.T) {
	t.Parallel()

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

	if err := f.putStats(s); err != nil {
		t.Fatal(err)
	}

	statsDir := filepath.Join(stateDir, "stats")
	statsFiles, err := listStatsFiles(statsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(statsFiles) != 1 {
		t.Fatalf("expected 1 stats file in stats directory, got %d", len(statsFiles))
	}

	statsFile := filepath.Join(statsDir, statsFiles[0])
	b, err := os.ReadFile(statsFile)
	if err != nil {
		t.Fatal(err)
	}

	var got stats
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, got, *s)

	indexContent, err := os.ReadFile(filepath.Join(statsDir, statsIndexFile))
	if err != nil {
		t.Fatal(err)
	}

	var summaries []statsSummary
	if err := json.Unmarshal(indexContent, &summaries); err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary in stats index, got %d", len(summaries))
	}
	testutil.AssertEqual(t, summaries[0].ID, strings.TrimSuffix(statsFiles[0], ".json"))
	testutil.AssertEqual(t, summaries[0].StartTime, s.StartTime)
	testutil.AssertEqual(t, summaries[0].MemoryUsage, s.MemoryUsage)
}

// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package stats

import (
	"encoding/json"
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
)

func TestStoreSaveRunAndListRuns(t *testing.T) {
	t.Parallel()

	store := OpenMemory(t.Name())
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("closing stats store: %v", err)
		}
	})

	if err := store.Bootstrap(t.Context()); err != nil {
		t.Fatal(err)
	}

	want := Run{
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

	if err := store.SaveRun(t.Context(), &want); err != nil {
		t.Fatal(err)
	}

	runs, err := store.ListRuns(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run in stats database, got %d", len(runs))
	}

	var got Run
	if err := json.Unmarshal(runs[0], &got); err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, got, want)

	db, err := store.open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	version, err := schemaVersion(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, version, currentSchemaVersion)
}

func TestStoreSaveRunUsesJSONB(t *testing.T) {
	t.Parallel()

	store := OpenMemory(t.Name())
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("closing stats store: %v", err)
		}
	})

	if err := store.Bootstrap(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := store.SaveRun(t.Context(), &Run{
		StartTime:  time.Date(2023, time.January, 1, 12, 0, 0, 0, time.UTC),
		TotalFeeds: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRun(t.Context(), &Run{
		StartTime:  time.Date(2023, time.January, 2, 12, 0, 0, 0, time.UTC),
		TotalFeeds: 2,
	}); err != nil {
		t.Fatal(err)
	}

	db, err := store.open(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	var payloadType string
	err = db.QueryRowContext(t.Context(), `SELECT typeof(payload_json) FROM runs LIMIT 1;`).Scan(&payloadType)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, payloadType, "blob")

	runs, err := store.ListRuns(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs in stats database, got %d", len(runs))
	}

	var got []Run
	for _, run := range runs {
		var item Run
		if err := json.Unmarshal(run, &item); err != nil {
			t.Fatal(err)
		}
		got = append(got, item)
	}

	testutil.AssertEqual(t, got[0].StartTime, time.Unix(1672660800, 0).UTC())
	testutil.AssertEqual(t, got[0].TotalFeeds, 2)
	testutil.AssertEqual(t, got[1].StartTime, time.Date(2023, time.January, 1, 12, 0, 0, 0, time.UTC))
	testutil.AssertEqual(t, got[1].TotalFeeds, 1)
}

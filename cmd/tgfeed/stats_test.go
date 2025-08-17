// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"go.astrophena.name/base/request"
	"go.astrophena.name/tools/cmd/tgfeed/internal/serviceaccount"
	"go.astrophena.name/tools/internal/rr"
)

// Updating this test:
//
//	$ SERVICE_ACCOUNT_FILE=... STATS_SPREADSHEET_ID=... go test -httprecord testdata/load/stats.httprr -run TestUploadStatsToSheets
//

func TestUploadStatsToSheets(t *testing.T) {
	var (
		key                  *serviceaccount.Key
		token                = "test"
		defaultSpreadsheetID = "123456789_ABCDEFGHIMgy0tHXlXy"
		spreadsheetID        = defaultSpreadsheetID
	)

	rec, err := rr.Open("testdata/load/stats.httprr", http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	rec.ScrubReq(func(r *http.Request) error {
		r.URL.Path = strings.ReplaceAll(r.URL.Path, spreadsheetID, defaultSpreadsheetID)
		r.Header.Del("Authorization")
		r.Header.Set("Authorization", "Bearer test")
		return nil
	})

	if rec.Recording() {
		spreadsheetID = os.Getenv("STATS_SPREADSHEET_ID")
		b, err := os.ReadFile(os.Getenv("SERVICE_ACCOUNT_FILE"))
		if err != nil {
			t.Fatal(err)
		}
		key, err = serviceaccount.LoadKey(b)
		if err != nil {
			t.Fatal(err)
		}
		token, err = key.AccessToken(t.Context(), request.DefaultClient, spreadsheetsScope)
		if err != nil {
			t.Fatal(err)
		}
	}

	f := &fetcher{
		httpc:              rec.Client(),
		logf:               t.Logf,
		statsSpreadsheetID: spreadsheetID,
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

	if err := f.uploadStatsToSheets(t.Context(), token, s); err != nil {
		t.Fatal(err)
	}
}

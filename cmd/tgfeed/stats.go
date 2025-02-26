// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.astrophena.name/base/request"
	"go.astrophena.name/base/version"
)

type stats struct {
	TotalFeeds       int `json:"total_feeds"`
	SuccessFeeds     int `json:"success_feeds"`
	FailedFeeds      int `json:"failed_feeds"`
	NotModifiedFeeds int `json:"not_modified_feeds"`

	StartTime        time.Time     `json:"start_time"`
	Duration         time.Duration `json:"duration"`
	TotalItemsParsed int           `json:"total_items_parsed"`

	TotalFetchTime time.Duration `json:"total_fetch_time"`
	AvgFetchTime   time.Duration `json:"avg_fetch_time"`

	MemoryUsage uint64 `json:"memory_usage"`
}

func (f *fetcher) reportStats(ctx context.Context, s *stats) error {
	sheetRange := f.statsSpreadsheetRange
	if sheetRange == "" {
		sheetRange = "Stats"
	}

	tok, err := f.serviceAccountKey.AccessToken(ctx, f.httpc, "https://www.googleapis.com/auth/spreadsheets")
	if err != nil {
		return err
	}

	// https://developers.google.com/sheets/api/reference/rest/v4/spreadsheets.values/append
	req := struct {
		Range          string     `json:"range"`
		MajorDimension string     `json:"majorDimension"`
		Values         [][]string `json:"values"`
	}{
		Range: sheetRange,
		// https://developers.google.com/sheets/api/reference/rest/v4/Dimension
		MajorDimension: "ROWS",
		Values: [][]string{
			{
				fmt.Sprintf("%d", s.TotalFeeds),
				fmt.Sprintf("%d", s.SuccessFeeds),
				fmt.Sprintf("%d", s.FailedFeeds),
				fmt.Sprintf("%d", s.NotModifiedFeeds),
				s.StartTime.Format(time.RFC3339),
				s.Duration.String(),
				fmt.Sprintf("%d", s.TotalItemsParsed),
				s.TotalFetchTime.String(),
				s.AvgFetchTime.String(),
				fmt.Sprintf("%d", s.MemoryUsage),
			},
		},
	}

	_, err = request.Make[any](ctx, request.Params{
		Method: http.MethodPost,
		// https://developers.google.com/sheets/api/reference/rest/v4/ValueInputOption
		URL:  "https://sheets.googleapis.com/v4/spreadsheets/" + f.statsSpreadsheetID + "/values/" + sheetRange + ":append?valueInputOption=USER_ENTERED",
		Body: req,
		Headers: map[string]string{
			"Authorization": "Bearer " + tok,
			"User-Agent":    version.UserAgent(),
		},
		HTTPClient: f.httpc,
		Scrubber:   f.scrubber,
	})
	return err
}

// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
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

func (f *fetcher) putStats(ctx context.Context, s *stats) error {
	statsDir := filepath.Join(f.stateDir, "stats")
	if err := os.MkdirAll(statsDir, 0o755); err != nil {
		return err
	}

	js, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	statsFile := filepath.Join(statsDir, time.Now().UTC().Format("20060102150405")+".json")

	return os.WriteFile(statsFile, js, 0o644)
}

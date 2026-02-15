// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const topFeedStatCount = 5

type stats struct {
	TotalFeeds       int `json:"total_feeds"`
	SuccessFeeds     int `json:"success_feeds"`
	FailedFeeds      int `json:"failed_feeds"`
	NotModifiedFeeds int `json:"not_modified_feeds"`

	StartTime        time.Time     `json:"start_time"`
	Duration         time.Duration `json:"duration"`
	TotalItemsParsed int           `json:"total_items_parsed"`

	TotalFetchTime time.Duration   `json:"total_fetch_time"`
	AvgFetchTime   time.Duration   `json:"avg_fetch_time"`
	FetchLatencyMS percentileStats `json:"fetch_latency_ms"`
	SendLatencyMS  percentileStats `json:"send_latency_ms"`

	HTTP2xxCount int `json:"http_2xx_count"`
	HTTP3xxCount int `json:"http_3xx_count"`
	HTTP4xxCount int `json:"http_4xx_count"`
	HTTP5xxCount int `json:"http_5xx_count"`

	TimeoutCount      int `json:"timeout_count"`
	NetworkErrorCount int `json:"network_error_count"`
	ParseErrorCount   int `json:"parse_error_count"`

	ItemsSeenTotal       int `json:"items_seen_total"`
	ItemsKeptTotal       int `json:"items_kept_total"`
	ItemsDedupedTotal    int `json:"items_deduped_total"`
	ItemsSkippedOldTotal int `json:"items_skipped_old_total"`
	ItemsEnqueuedTotal   int `json:"items_enqueued_total"`

	MessagesAttempted int `json:"messages_attempted"`
	MessagesSent      int `json:"messages_sent"`
	MessagesFailed    int `json:"messages_failed"`

	FetchRetriesTotal       int           `json:"fetch_retries_total"`
	FeedsRetriedCount       int           `json:"feeds_retried_count"`
	BackoffSleepTotal       time.Duration `json:"backoff_sleep_total"`
	SpecialRateLimitRetries int           `json:"special_rate_limit_retries"`

	SeenItemsEntriesTotal int `json:"seen_items_entries_total"`
	SeenItemsPrunedTotal  int `json:"seen_items_pruned_total"`
	StateBytesWritten     int `json:"state_bytes_written"`

	TopSlowestFeeds []feedStatsSummary `json:"top_slowest_feeds,omitempty"`
	TopErrorFeeds   []feedStatsSummary `json:"top_error_feeds,omitempty"`
	TopNewItemFeeds []feedStatsSummary `json:"top_new_item_feeds,omitempty"`

	MemoryUsage uint64 `json:"memory_usage"`

	fetchLatencySamples []time.Duration       `json:"-"`
	sendLatencySamples  []time.Duration       `json:"-"`
	FeedStatsByURL      map[string]*feedStats `json:"-"`
}

type percentileStats struct {
	P50 int64 `json:"p50"`
	P90 int64 `json:"p90"`
	P99 int64 `json:"p99"`
	Max int64 `json:"max"`
}

type feedStats struct {
	URL             string
	FetchDuration   time.Duration
	Failures        int
	ItemsEnqueued   int
	Retries         int
	LastStatusClass int
}

type feedStatsSummary struct {
	URL             string        `json:"url"`
	FetchDuration   time.Duration `json:"fetch_duration"`
	Failures        int           `json:"failures"`
	ItemsEnqueued   int           `json:"items_enqueued"`
	Retries         int           `json:"retries"`
	LastStatusClass int           `json:"last_status_class"`
}

func (s *stats) feedStats(url string) *feedStats {
	if s.FeedStatsByURL == nil {
		s.FeedStatsByURL = make(map[string]*feedStats)
	}
	if _, ok := s.FeedStatsByURL[url]; !ok {
		s.FeedStatsByURL[url] = &feedStats{URL: url}
	}
	return s.FeedStatsByURL[url]
}

func quantilesMS(samples []time.Duration) percentileStats {
	if len(samples) == 0 {
		return percentileStats{}
	}
	ms := make([]int64, len(samples))
	for i, sample := range samples {
		ms[i] = sample.Milliseconds()
	}
	slices.Sort(ms)
	return percentileStats{
		P50: percentileValue(ms, 0.50),
		P90: percentileValue(ms, 0.90),
		P99: percentileValue(ms, 0.99),
		Max: ms[len(ms)-1],
	}
}

func percentileValue(sorted []int64, percentile float64) int64 {
	idx := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	idx = max(0, min(idx, len(sorted)-1))
	return sorted[idx]
}

func topFeedStats(input map[string]*feedStats, less func(a *feedStats, b *feedStats) int) []feedStatsSummary {
	all := make([]*feedStats, 0, len(input))
	for _, item := range input {
		all = append(all, item)
	}
	slices.SortFunc(all, less)
	all = all[:min(topFeedStatCount, len(all))]
	result := make([]feedStatsSummary, 0, len(all))
	for _, item := range all {
		result = append(result, feedStatsSummary{
			URL:             item.URL,
			FetchDuration:   item.FetchDuration,
			Failures:        item.Failures,
			ItemsEnqueued:   item.ItemsEnqueued,
			Retries:         item.Retries,
			LastStatusClass: item.LastStatusClass,
		})
	}
	return result
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

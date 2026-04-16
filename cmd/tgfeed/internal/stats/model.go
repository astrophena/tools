// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package stats aggregates tgfeed runtime metrics and persists run snapshots.
package stats

import (
	"context"
	"errors"
	"math"
	"net"
	"slices"
	"time"
)

const topFeedStatCount = 5

// Run stores aggregated statistics for one tgfeed execution.
type Run struct {
	TotalFeeds       int `json:"total_feeds"`
	SuccessFeeds     int `json:"success_feeds"`
	FailedFeeds      int `json:"failed_feeds"`
	NotModifiedFeeds int `json:"not_modified_feeds"`

	StartTime        time.Time     `json:"start_time"`
	Duration         time.Duration `json:"duration"`
	TotalItemsParsed int           `json:"total_items_parsed"`

	TotalFetchTime time.Duration   `json:"total_fetch_time"`
	AvgFetchTime   time.Duration   `json:"avg_fetch_time"`
	FetchLatencyMS PercentileStats `json:"fetch_latency_ms"`
	SendLatencyMS  PercentileStats `json:"send_latency_ms"`

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

	TopSlowestFeeds []FeedStatsSummary `json:"top_slowest_feeds,omitempty"`
	TopErrorFeeds   []FeedStatsSummary `json:"top_error_feeds,omitempty"`
	TopNewItemFeeds []FeedStatsSummary `json:"top_new_item_feeds,omitempty"`

	MemoryUsage uint64 `json:"memory_usage"`

	FetchLatencySamples []time.Duration       `json:"-"`
	SendLatencySamples  []time.Duration       `json:"-"`
	FeedStatsByURL      map[string]*FeedStats `json:"-"`
}

// PercentileStats contains percentile latency values in milliseconds.
type PercentileStats struct {
	P50 int64 `json:"p50"`
	P90 int64 `json:"p90"`
	P99 int64 `json:"p99"`
	Max int64 `json:"max"`
}

// FeedStats stores per-feed counters used for top-N summaries.
type FeedStats struct {
	URL             string
	FetchDuration   time.Duration
	Failures        int
	ItemsEnqueued   int
	Retries         int
	LastStatusClass int
}

// FeedStatsSummary stores feed metrics exposed in the public JSON payload.
type FeedStatsSummary struct {
	URL             string        `json:"url"`
	FetchDuration   time.Duration `json:"fetch_duration"`
	Failures        int           `json:"failures"`
	ItemsEnqueued   int           `json:"items_enqueued"`
	Retries         int           `json:"retries"`
	LastStatusClass int           `json:"last_status_class"`
}

// FeedItemDecisionReason classifies why an item was skipped.
type FeedItemDecisionReason int

const (
	FeedItemSkipReasonUnknown FeedItemDecisionReason = iota
	FeedItemSkipReasonOld
	FeedItemSkipReasonSeen
)

// FeedStats returns mutable per-feed statistics.
func (r *Run) FeedStats(url string) *FeedStats {
	if r.FeedStatsByURL == nil {
		r.FeedStatsByURL = make(map[string]*FeedStats)
	}
	if _, ok := r.FeedStatsByURL[url]; !ok {
		r.FeedStatsByURL[url] = &FeedStats{URL: url}
	}
	return r.FeedStatsByURL[url]
}

// QuantilesMS returns latency percentiles in milliseconds.
func QuantilesMS(samples []time.Duration) PercentileStats {
	if len(samples) == 0 {
		return PercentileStats{}
	}
	ms := make([]int64, len(samples))
	for i, sample := range samples {
		ms[i] = sample.Milliseconds()
	}
	slices.Sort(ms)
	return PercentileStats{
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

// TopFeedStats calculates top-N feed summaries according to provided comparator.
func TopFeedStats(input map[string]*FeedStats, less func(a *FeedStats, b *FeedStats) int) []FeedStatsSummary {
	all := make([]*FeedStats, 0, len(input))
	for _, item := range input {
		all = append(all, item)
	}
	slices.SortFunc(all, less)
	all = all[:min(topFeedStatCount, len(all))]
	result := make([]FeedStatsSummary, 0, len(all))
	for _, item := range all {
		result = append(result, FeedStatsSummary{
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

// AddHTTPStatusClass increments status-class counters for a feed response.
func (r *Run) AddHTTPStatusClass(url string, statusCode int) {
	class := statusCode / 100
	r.FeedStats(url).LastStatusClass = class
	switch class {
	case 2:
		r.HTTP2xxCount += 1
	case 3:
		r.HTTP3xxCount += 1
	case 4:
		r.HTTP4xxCount += 1
	case 5:
		r.HTTP5xxCount += 1
	}
}

// ClassifyFailure increments failure counters based on error type.
func (r *Run) ClassifyFailure(url string, err error) {
	if netErr, ok := errors.AsType[net.Error](err); ok {
		r.NetworkErrorCount += 1
		if netErr.Timeout() {
			r.TimeoutCount += 1
		}
		r.FeedStats(url).LastStatusClass = 0
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		r.TimeoutCount += 1
		r.NetworkErrorCount += 1
		r.FeedStats(url).LastStatusClass = 0
		return
	}
	r.FeedStats(url).LastStatusClass = 0
}

// RecordItemDecision counts item skip reasons.
func (r *Run) RecordItemDecision(reason FeedItemDecisionReason) {
	switch reason {
	case FeedItemSkipReasonOld:
		r.ItemsSkippedOldTotal += 1
	case FeedItemSkipReasonSeen:
		r.ItemsDedupedTotal += 1
	}
}

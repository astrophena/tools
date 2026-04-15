// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.astrophena.name/base/safefile"
)

const (
	topFeedStatCount   = 5
	statsIndexFile     = "index.json"
	statsIndexMaxItems = 1000
)

// Stats model.

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

type statsSummary struct {
	ID string `json:"id"`

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

	MemoryUsage uint64 `json:"memory_usage"`
}

// Aggregation helpers.

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

func (s *stats) addHTTPStatusClass(url string, statusCode int) {
	class := statusCode / 100
	s.feedStats(url).LastStatusClass = class
	switch class {
	case 2:
		s.HTTP2xxCount += 1
	case 3:
		s.HTTP3xxCount += 1
	case 4:
		s.HTTP4xxCount += 1
	case 5:
		s.HTTP5xxCount += 1
	}
}

func (s *stats) classifyFailure(url string, err error) {
	if netErr, ok := errors.AsType[net.Error](err); ok {
		s.NetworkErrorCount += 1
		if netErr.Timeout() {
			s.TimeoutCount += 1
		}
		s.feedStats(url).LastStatusClass = 0
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		s.TimeoutCount += 1
		s.NetworkErrorCount += 1
		s.feedStats(url).LastStatusClass = 0
		return
	}
	s.feedStats(url).LastStatusClass = 0
}

func (s *stats) recordItemDecision(decision feedItemDecision) {
	switch decision.reason {
	case feedItemSkipReasonOld:
		s.ItemsSkippedOldTotal += 1
	case feedItemSkipReasonSeen:
		s.ItemsDedupedTotal += 1
	}
}

// Persistence.

func (f *fetcher) putStats(s *stats) error {
	statsDir := filepath.Join(f.stateDir, "stats")
	if err := os.MkdirAll(statsDir, 0o755); err != nil {
		return err
	}

	id := time.Now().UTC().Format("20060102150405")
	js, err := json.Marshal(s)
	if err != nil {
		return err
	}
	statsFile := filepath.Join(statsDir, id+".json")
	if err := safefile.WriteFile(statsFile, js, 0o644); err != nil {
		return err
	}

	return putStatsIndex(statsDir, s.summary(id))
}

func (s *stats) summary(id string) statsSummary {
	return statsSummary{
		ID: id,

		TotalFeeds:       s.TotalFeeds,
		SuccessFeeds:     s.SuccessFeeds,
		FailedFeeds:      s.FailedFeeds,
		NotModifiedFeeds: s.NotModifiedFeeds,

		StartTime:        s.StartTime,
		Duration:         s.Duration,
		TotalItemsParsed: s.TotalItemsParsed,

		TotalFetchTime: s.TotalFetchTime,
		AvgFetchTime:   s.AvgFetchTime,
		FetchLatencyMS: s.FetchLatencyMS,
		SendLatencyMS:  s.SendLatencyMS,

		HTTP2xxCount: s.HTTP2xxCount,
		HTTP3xxCount: s.HTTP3xxCount,
		HTTP4xxCount: s.HTTP4xxCount,
		HTTP5xxCount: s.HTTP5xxCount,

		TimeoutCount:      s.TimeoutCount,
		NetworkErrorCount: s.NetworkErrorCount,
		ParseErrorCount:   s.ParseErrorCount,

		ItemsSeenTotal:       s.ItemsSeenTotal,
		ItemsKeptTotal:       s.ItemsKeptTotal,
		ItemsDedupedTotal:    s.ItemsDedupedTotal,
		ItemsSkippedOldTotal: s.ItemsSkippedOldTotal,
		ItemsEnqueuedTotal:   s.ItemsEnqueuedTotal,

		MessagesAttempted: s.MessagesAttempted,
		MessagesSent:      s.MessagesSent,
		MessagesFailed:    s.MessagesFailed,

		FetchRetriesTotal:       s.FetchRetriesTotal,
		FeedsRetriedCount:       s.FeedsRetriedCount,
		BackoffSleepTotal:       s.BackoffSleepTotal,
		SpecialRateLimitRetries: s.SpecialRateLimitRetries,

		SeenItemsEntriesTotal: s.SeenItemsEntriesTotal,
		SeenItemsPrunedTotal:  s.SeenItemsPrunedTotal,
		StateBytesWritten:     s.StateBytesWritten,

		MemoryUsage: s.MemoryUsage,
	}
}

func putStatsIndex(statsDir string, summary statsSummary) error {
	summaries, err := loadStatsIndex(statsDir)
	if err != nil {
		return err
	}

	result := make([]statsSummary, 0, min(len(summaries)+1, statsIndexMaxItems))
	result = append(result, summary)
	for _, item := range summaries {
		if item.ID == summary.ID {
			continue
		}
		result = append(result, item)
		if len(result) >= statsIndexMaxItems {
			break
		}
	}

	content, err := json.Marshal(result)
	if err != nil {
		return err
	}

	return safefile.WriteFile(filepath.Join(statsDir, statsIndexFile), content, 0o644)
}

func loadStatsIndex(statsDir string) ([]statsSummary, error) {
	indexPath := filepath.Join(statsDir, statsIndexFile)
	content, err := os.ReadFile(indexPath)
	if err == nil {
		var summaries []statsSummary
		if err := json.Unmarshal(content, &summaries); err == nil {
			return summaries, nil
		}
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return rebuildStatsIndex(statsDir)
}

func rebuildStatsIndex(statsDir string) ([]statsSummary, error) {
	statsFiles, err := listStatsFiles(statsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	summaries := make([]statsSummary, 0, min(len(statsFiles), statsIndexMaxItems))
	for _, name := range statsFiles[:min(len(statsFiles), statsIndexMaxItems)] {
		summary, err := readStatsSummary(filepath.Join(statsDir, name), strings.TrimSuffix(name, ".json"))
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func listStatsFiles(statsDir string) ([]string, error) {
	entries, err := os.ReadDir(statsDir)
	if err != nil {
		return nil, err
	}

	statsFiles := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || entry.Name() == statsIndexFile {
			continue
		}
		statsFiles = append(statsFiles, entry.Name())
	}

	slices.SortFunc(statsFiles, func(a, b string) int {
		return strings.Compare(b, a)
	})
	return statsFiles, nil
}

func readStatsSummary(path string, id string) (statsSummary, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return statsSummary{}, err
	}

	var summary statsSummary
	if err := json.Unmarshal(content, &summary); err != nil {
		return statsSummary{}, err
	}
	summary.ID = id
	return summary, nil
}

// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package components

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"time"

	"go.astrophena.name/base/humanfmt"
	"go.astrophena.name/tools/cmd/tgfeed/internal/stats"
)

type feedDisplay struct{ Name, Note, Value string }

func selectedIndex(p StatsProps) int {
	for i, run := range p.Runs {
		if run.StartedAtUnix == p.SelectedAt {
			return i
		}
	}
	return 0
}

func previousRun(p StatsProps) (stats.RunSummary, bool) {
	// Health indicators always compare the latest two runs, even when the
	// detailed explorer is pinned to an older run.
	if len(p.Runs) < 2 {
		return stats.RunSummary{}, false
	}
	return p.Runs[1], true
}

func runContextLabel(p StatsProps) string {
	if p.SelectedAt != 0 {
		return "Pinned run"
	}
	return "Latest run"
}

func slowFeeds(run *stats.Run) []feedDisplay {
	if run == nil {
		return nil
	}
	items := make([]feedDisplay, 0, len(run.TopSlowestFeeds))
	for _, feed := range run.TopSlowestFeeds {
		items = append(items, feedDisplay{
			Name:  feed.URL,
			Note:  fmt.Sprintf("Status class: %d", feed.LastStatusClass),
			Value: formatDuration(feed.FetchDuration),
		})
	}
	return items
}

func errorFeeds(run *stats.Run) []feedDisplay {
	if run == nil {
		return nil
	}
	items := make([]feedDisplay, 0, len(run.TopErrorFeeds))
	for _, feed := range run.TopErrorFeeds {
		items = append(items, feedDisplay{
			Name:  feed.URL,
			Note:  fmt.Sprintf("Failures: %d · Retries: %d", feed.Failures, feed.Retries),
			Value: strconv.Itoa(feed.Failures),
		})
	}
	return items
}

func itemFeeds(run *stats.Run) []feedDisplay {
	if run == nil {
		return nil
	}
	items := make([]feedDisplay, 0, len(run.TopNewItemFeeds))
	for _, feed := range run.TopNewItemFeeds {
		items = append(items, feedDisplay{
			Name:  feed.URL,
			Note:  fmt.Sprintf("Retries: %d", feed.Retries),
			Value: fmt.Sprintf("%d items", feed.ItemsEnqueued),
		})
	}
	return items
}

func healthyFeeds(run stats.RunSummary) int { return run.SuccessFeeds + run.NotModifiedFeeds }

func percent(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

func health(runs []stats.RunSummary) (string, string, string) {
	if len(runs) == 0 {
		return "No data", "unknown", "No persisted run snapshots are available yet."
	}
	run := runs[0]
	if run.TotalFeeds <= 0 {
		return "Idle", "unknown", "Latest run processed zero feeds."
	}
	healthy := healthyFeeds(run)
	rate := percent(healthy, run.TotalFeeds)
	delivery := percent(run.MessagesSent, run.MessagesAttempted)
	if rate >= 98 && run.FailedFeeds == 0 {
		return "Healthy", "healthy", fmt.Sprintf("Healthy feeds: %d/%d (%.1f%%) · Delivery: %s", healthy, run.TotalFeeds, rate, formatPercent(delivery, run.MessagesAttempted > 0))
	}
	if rate >= 90 {
		return "Degraded", "degraded", fmt.Sprintf("Healthy feeds: %d/%d (%.1f%%) · Failures: %d", healthy, run.TotalFeeds, rate, run.FailedFeeds)
	}
	return "Failing", "failing", fmt.Sprintf("Healthy feeds: %d/%d (%.1f%%) · Failures: %d", healthy, run.TotalFeeds, rate, run.FailedFeeds)
}

func delta(latest, previous float64, hasPrevious bool, betterHigher bool, precision int, unit string) (string, string) {
	if !hasPrevious {
		return "vs prev run: n/a", "neutral"
	}
	difference := latest - previous
	if math.Abs(difference) < 0.0001 {
		return "no change vs prev run", "neutral"
	}
	// Some metrics improve as they grow (health and delivery), while latency,
	// failures, duration, and memory improve as they shrink.
	tone := "bad"
	if (difference > 0) == betterHigher {
		tone = "good"
	}
	return fmt.Sprintf("%+.*f%s vs prev run", precision, difference, unit), tone
}

func memoryDelta(latest, previous uint64, hasPrevious bool) (string, string) {
	if !hasPrevious {
		return "vs prev run: n/a", "neutral"
	}
	if latest == previous {
		return "no change vs prev run", "neutral"
	}
	if latest > previous {
		difference := latest - previous
		if difference < 1024 {
			return "no change vs prev run", "neutral"
		}
		return "+" + humanfmt.Bytes(difference) + " vs prev run", "bad"
	}
	difference := previous - latest
	if difference < 1024 {
		return "no change vs prev run", "neutral"
	}
	return "-" + humanfmt.Bytes(difference) + " vs prev run", "good"
}

func durationDelta(latest, previous time.Duration, hasPrevious bool) (string, string) {
	if !hasPrevious {
		return "vs prev run: n/a", "neutral"
	}
	if latest == previous {
		return "no change vs prev run", "neutral"
	}
	if latest > previous {
		return "+" + formatDuration(latest-previous) + " vs prev run", "bad"
	}
	return "-" + formatDuration(previous-latest) + " vs prev run", "good"
}

func statsURL(startedAt int64, autoRefresh, details bool) string {
	// Every stats interaction rebuilds its URL from the complete view state so
	// pinning, auto-refresh, and expanded details survive fragment swaps.
	query := url.Values{}
	if startedAt != 0 {
		query.Set("started_at_unix", strconv.FormatInt(startedAt, 10))
	}
	if autoRefresh {
		query.Set("auto_refresh", "true")
	}
	if details {
		query.Set("details", "true")
	}
	if encoded := query.Encode(); encoded != "" {
		return "/stats?" + encoded
	}
	return "/stats"
}

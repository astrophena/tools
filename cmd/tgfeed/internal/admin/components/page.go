// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package components

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"time"

	"go.astrophena.name/tools/cmd/tgfeed/internal/stats"
)

const (
	FragmentAppShell         = "app-shell"
	FragmentDashboardContent = "dashboard-content"
	FragmentStatsContent     = "stats-content"
	FragmentConfigPanel      = "config-panel"
	FragmentErrorPanel       = "error-template-panel"
)

// PageProps contains the shared application page state.
type PageProps struct {
	Title         string
	Route         string
	Banner        string
	JS            string
	CSS           string
	Icon          string
	Logo          string
	Stats         *StatsProps
	Configuration *ConfigurationProps
}

// StatsProps contains data rendered by the statistics dashboard.
type StatsProps struct {
	Runs        []stats.RunSummary
	Active      *stats.Run
	SelectedAt  int64
	AutoRefresh bool
	DetailsOpen bool
	RefreshedAt time.Time
	Error       string
}

// ConfigurationProps contains the two editable resources.
type ConfigurationProps struct {
	Config        EditorProps
	ErrorTemplate EditorProps
}

// EditorProps describes one CodeMirror-enhanced text resource.
type EditorProps struct {
	ID          string
	Name        string
	Title       string
	Description string
	Placeholder string
	Language    string
	Value       string
	Baseline    string
	Error       string
	SaveURL     string
}

func pageTitle(title string) string {
	if title == "" {
		return "tgfeed"
	}
	return title + " · tgfeed"
}

func routeURL(route string) string {
	if route == "configuration" {
		return "/config"
	}
	return "/stats"
}

func editorStatus(p EditorProps) string {
	if p.Value != p.Baseline {
		return "Unsaved"
	}
	return "Synced"
}

func selectedIndex(p StatsProps) int {
	for i, r := range p.Runs {
		if r.StartedAtUnix == p.SelectedAt {
			return i
		}
	}
	return 0
}

func previousRun(p StatsProps) (stats.RunSummary, bool) {
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

func pinURL(p StatsProps) string {
	started := p.SelectedAt
	if started == 0 && len(p.Runs) > 1 {
		started = p.Runs[1].StartedAtUnix
	}
	return statsURL(started, p.AutoRefresh, p.DetailsOpen)
}

type feedDisplay struct{ Name, Note, Value string }

func slowFeeds(r *stats.Run) []feedDisplay {
	if r == nil {
		return nil
	}
	items := make([]feedDisplay, 0, len(r.TopSlowestFeeds))
	for _, x := range r.TopSlowestFeeds {
		items = append(items, feedDisplay{x.URL, fmt.Sprintf("Status class: %d", x.LastStatusClass), formatDuration(x.FetchDuration)})
	}
	return items
}

func errorFeeds(r *stats.Run) []feedDisplay {
	if r == nil {
		return nil
	}
	items := make([]feedDisplay, 0, len(r.TopErrorFeeds))
	for _, x := range r.TopErrorFeeds {
		items = append(items, feedDisplay{x.URL, fmt.Sprintf("Failures: %d · Retries: %d", x.Failures, x.Retries), strconv.Itoa(x.Failures)})
	}
	return items
}

func itemFeeds(r *stats.Run) []feedDisplay {
	if r == nil {
		return nil
	}
	items := make([]feedDisplay, 0, len(r.TopNewItemFeeds))
	for _, x := range r.TopNewItemFeeds {
		items = append(items, feedDisplay{x.URL, fmt.Sprintf("Retries: %d", x.Retries), fmt.Sprintf("%d items", x.ItemsEnqueued)})
	}
	return items
}

func formatDateTime(t time.Time) string {
	if t.IsZero() {
		return "n/a"
	}
	return t.UTC().Format("02.01.2006, 15:04:05 UTC")
}

func formatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return "n/a"
	}
	d := time.Since(t)
	if d < 0 {
		return "just now"
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		return "n/a"
	}
	if d < time.Second {
		return fmt.Sprintf("%.0f ms", float64(d)/float64(time.Millisecond))
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1f s", d.Seconds())
	}
	return fmt.Sprintf("%dm %.0fs", int(d.Minutes()), math.Mod(d.Seconds(), 60))
}

func formatBytes(n uint64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

func healthyFeeds(r stats.RunSummary) int { return r.SuccessFeeds + r.NotModifiedFeeds }

func percent(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

func formatPercent(v float64, valid bool) string {
	if !valid {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", v)
}

func health(runs []stats.RunSummary) (string, string, string) {
	if len(runs) == 0 {
		return "No data", "unknown", "No persisted run snapshots are available yet."
	}
	r := runs[0]
	if r.TotalFeeds <= 0 {
		return "Idle", "unknown", "Latest run processed zero feeds."
	}
	h := healthyFeeds(r)
	rate := percent(h, r.TotalFeeds)
	delivery := percent(r.MessagesSent, r.MessagesAttempted)
	if rate >= 98 && r.FailedFeeds == 0 {
		return "Healthy", "healthy", fmt.Sprintf("Healthy feeds: %d/%d (%.1f%%) · Delivery: %s", h, r.TotalFeeds, rate, formatPercent(delivery, r.MessagesAttempted > 0))
	}
	if rate >= 90 {
		return "Degraded", "degraded", fmt.Sprintf("Healthy feeds: %d/%d (%.1f%%) · Failures: %d", h, r.TotalFeeds, rate, r.FailedFeeds)
	}
	return "Failing", "failing", fmt.Sprintf("Healthy feeds: %d/%d (%.1f%%) · Failures: %d", h, r.TotalFeeds, rate, r.FailedFeeds)
}

func delta(latest, previous float64, hasPrevious bool, betterHigher bool, precision int, unit string) (string, string) {
	if !hasPrevious {
		return "vs prev run: n/a", "neutral"
	}
	d := latest - previous
	if math.Abs(d) < 0.0001 {
		return "no change vs prev run", "neutral"
	}
	tone := "bad"
	if (d > 0) == betterHigher {
		tone = "good"
	}
	return fmt.Sprintf("%+.*f%s vs prev run", precision, d, unit), tone
}

func statsURL(startedAt int64, autoRefresh, details bool) string {
	v := url.Values{}
	if startedAt != 0 {
		v.Set("started_at_unix", strconv.FormatInt(startedAt, 10))
	}
	if autoRefresh {
		v.Set("auto_refresh", "true")
	}
	if details {
		v.Set("details", "true")
	}
	if q := v.Encode(); q != "" {
		return "/stats?" + q
	}
	return "/stats"
}

type chartSpec struct {
	Config    map[string]any `json:"config"`
	Preset    string         `json:"preset,omitempty"`
	Times     []time.Time    `json:"times,omitempty"`
	SelectURL []string       `json:"select_urls,omitempty"`
}

func chartJSON(spec chartSpec) string {
	b, err := json.Marshal(spec)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func timelineChart(p StatsProps) string {
	runs := append([]stats.RunSummary(nil), p.Runs...)
	if len(runs) > 20 {
		runs = runs[:20]
	}
	labels, values, times, urls := []string{}, []float64{}, []time.Time{}, []string{}
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		labels = append(labels, r.StartTime.UTC().Format("02.01 15:04"))
		values = append(values, percent(healthyFeeds(r), r.TotalFeeds))
		times = append(times, r.StartTime)
		urls = append(urls, statsURL(r.StartedAtUnix, p.AutoRefresh, p.DetailsOpen))
	}
	o := map[string]any{}
	o["interaction"] = map[string]any{"mode": "index", "intersect": false}
	return chartJSON(chartSpec{Preset: "health", Times: times, SelectURL: urls, Config: map[string]any{"type": "line", "data": map[string]any{"labels": labels, "datasets": []any{map[string]any{"label": "Healthy feeds (%)", "data": values, "borderColor": "#6fe1b7", "backgroundColor": "rgba(111, 225, 183, 0.2)", "tension": .32, "fill": true}}}, "options": o}})
}

func barChart(labels []string, datasets []any, preset string) string {
	return chartJSON(chartSpec{Preset: preset, Config: map[string]any{"type": "bar", "data": map[string]any{"labels": labels, "datasets": datasets}}})
}

func detailsCharts(p StatsProps) []string {
	r := p.Active
	if r == nil {
		return nil
	}
	runs := append([]stats.RunSummary(nil), p.Runs...)
	if len(runs) > 20 {
		runs = runs[:20]
	}
	labels, healthy, failed, memory := []string{}, []int{}, []int{}, []uint64{}
	for i := len(runs) - 1; i >= 0; i-- {
		x := runs[i]
		labels = append(labels, x.StartTime.UTC().Format("02.01 15:04"))
		healthy = append(healthy, healthyFeeds(x))
		failed = append(failed, x.FailedFeeds)
		memory = append(memory, x.MemoryUsage)
	}
	pending := max(r.MessagesAttempted-r.MessagesSent-r.MessagesFailed, 0)
	return []string{
		barChart(labels, []any{map[string]any{"label": "Healthy", "data": healthy, "backgroundColor": "rgba(122, 223, 172, 0.8)"}, map[string]any{"label": "Failed", "data": failed, "backgroundColor": "rgba(255, 127, 136, 0.85)"}}, ""),
		barChart([]string{"Success", "Not changed", "Failed"}, []any{map[string]any{"label": "Feeds", "data": []int{r.SuccessFeeds, r.NotModifiedFeeds, r.FailedFeeds}, "backgroundColor": []string{"#6de2b7", "#72d7f6", "#ff8e95"}}}, ""),
		barChart([]string{"Sent", "Failed", "Pending"}, []any{map[string]any{"label": "Delivery", "data": []int{r.MessagesSent, r.MessagesFailed, pending}, "backgroundColor": []string{"#6de2b7", "#ff8e95", "#6b93f7"}}}, ""),
		barChart([]string{"2xx", "3xx", "4xx", "5xx"}, []any{map[string]any{"label": "HTTP", "data": []int{r.HTTP2xxCount, r.HTTP3xxCount, r.HTTP4xxCount, r.HTTP5xxCount}, "backgroundColor": []string{"#6de2b7", "#72d7f6", "#f0be6e", "#ff8e95"}}}, ""),
		barChart([]string{"P50", "P90", "P99", "Max"}, []any{map[string]any{"label": "Fetch latency (s)", "data": []float64{float64(r.FetchLatencyMS.P50) / 1000, float64(r.FetchLatencyMS.P90) / 1000, float64(r.FetchLatencyMS.P99) / 1000, float64(r.FetchLatencyMS.Max) / 1000}, "backgroundColor": "rgba(111, 225, 183, 0.72)"}, map[string]any{"label": "Send latency (s)", "data": []float64{float64(r.SendLatencyMS.P50) / 1000, float64(r.SendLatencyMS.P90) / 1000, float64(r.SendLatencyMS.P99) / 1000, float64(r.SendLatencyMS.Max) / 1000}, "backgroundColor": "rgba(126, 167, 255, 0.7)"}}, ""),
		barChart([]string{"Timeout", "Network", "Parse", "Retries", "Rate limit retries"}, []any{map[string]any{"label": "Counts", "data": []int{r.TimeoutCount, r.NetworkErrorCount, r.ParseErrorCount, r.FetchRetriesTotal, r.SpecialRateLimitRetries}, "backgroundColor": "rgba(255, 142, 149, 0.72)"}}, ""),
		barChart(labels, []any{map[string]any{"label": "Memory Usage", "data": memory, "backgroundColor": "rgba(180, 150, 255, 0.72)"}}, "bytes"),
	}
}

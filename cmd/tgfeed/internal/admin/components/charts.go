// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package components

import (
	"encoding/json"
	"fmt"
	"time"

	"go.astrophena.name/tools/cmd/tgfeed/internal/stats"
)

type chartSpec struct {
	Config    chartConfig `json:"config"`
	Preset    string      `json:"preset,omitempty"`
	Times     []time.Time `json:"times,omitempty"`
	SelectURL []string    `json:"select_urls,omitempty"`
}

// The chart types below model only the Chart.js fields emitted
// by the server. Keeping this boundary typed makes additions to the dashboard
// visible to the compiler instead of hiding them in generic JSON maps.

type chartConfig struct {
	Type    string       `json:"type"`
	Data    chartData    `json:"data"`
	Options chartOptions `json:"options,omitempty"`
}

type chartData struct {
	Labels   []string       `json:"labels"`
	Datasets []chartDataset `json:"datasets"`
}

type chartDataset struct {
	Label           string      `json:"label"`
	Data            []float64   `json:"data"`
	BackgroundColor chartColors `json:"backgroundColor"`
	BorderColor     string      `json:"borderColor,omitempty"`
	Tension         float64     `json:"tension,omitempty"`
	Fill            bool        `json:"fill,omitempty"`
}

type chartOptions struct {
	Interaction *chartInteraction `json:"interaction,omitempty"`
}

type chartInteraction struct {
	Mode      string `json:"mode"`
	Intersect bool   `json:"intersect"`
}

type chartColors []string

func color(value string) chartColors { return chartColors{value} }

func colors(values ...string) chartColors { return chartColors(values) }

func (c chartColors) MarshalJSON() ([]byte, error) {
	// Chart.js accepts either one color for the whole dataset or one color per
	// data point. Preserve that distinction in JSON without weakening the Go
	// model to an untyped value.
	if len(c) == 1 {
		return json.Marshal(c[0])
	}
	return json.Marshal([]string(c))
}

type detailCharts struct {
	FeedHealth  string
	Composition string
	Delivery    string
	HTTPStatus  string
	Latency     string
	Errors      string
	Memory      string
}

func chartJSON(spec chartSpec) string {
	b, err := json.Marshal(spec)
	if err != nil {
		// Every field in chartSpec has a fixed JSON representation. A failure is
		// therefore a programming error, not bad runtime data that can be shown
		// meaningfully in the dashboard.
		panic(fmt.Sprintf("marshal chart specification: %v", err))
	}
	return string(b)
}

func timelineChart(p StatsProps) string {
	runs := recentRuns(p.Runs)
	labels := make([]string, 0, len(runs))
	values := make([]float64, 0, len(runs))
	times := make([]time.Time, 0, len(runs))
	urls := make([]string, 0, len(runs))
	// The store returns newest first, while charts read naturally from oldest
	// to newest along the horizontal axis.
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		labels = append(labels, run.StartTime.UTC().Format("02.01 15:04"))
		values = append(values, percent(healthyFeeds(run), run.TotalFeeds))
		times = append(times, run.StartTime)
		urls = append(urls, statsURL(run.StartedAtUnix, p.AutoRefresh, p.DetailsOpen))
	}
	return chartJSON(chartSpec{
		Preset:    "health",
		Times:     times,
		SelectURL: urls,
		Config: chartConfig{
			Type: "line",
			Data: chartData{
				Labels: labels,
				Datasets: []chartDataset{
					{
						Label:           "Healthy feeds (%)",
						Data:            values,
						BorderColor:     "#6fe1b7",
						BackgroundColor: color("rgba(111, 225, 183, 0.2)"),
						Tension:         0.32,
						Fill:            true,
					},
				},
			},
			Options: chartOptions{
				Interaction: &chartInteraction{
					Mode: "index",
				},
			},
		},
	})
}

func detailsCharts(p StatsProps) detailCharts {
	run := p.Active
	if run == nil {
		return detailCharts{}
	}
	runs := recentRuns(p.Runs)
	labels := make([]string, 0, len(runs))
	healthy := make([]float64, 0, len(runs))
	failed := make([]float64, 0, len(runs))
	memory := make([]float64, 0, len(runs))
	for i := len(runs) - 1; i >= 0; i-- {
		summary := runs[i]
		labels = append(labels, summary.StartTime.UTC().Format("02.01 15:04"))
		healthy = append(healthy, float64(healthyFeeds(summary)))
		failed = append(failed, float64(summary.FailedFeeds))
		memory = append(memory, float64(summary.MemoryUsage))
	}
	pending := max(run.MessagesAttempted-run.MessagesSent-run.MessagesFailed, 0)

	feedHealth := barChart(
		labels,
		dataset("Healthy", healthy, color("rgba(122, 223, 172, 0.8)")),
		dataset("Failed", failed, color("rgba(255, 127, 136, 0.85)")),
	)
	composition := barChart(
		[]string{"Success", "Not changed", "Failed"},
		dataset(
			"Feeds",
			numbers(run.SuccessFeeds, run.NotModifiedFeeds, run.FailedFeeds),
			colors("#6de2b7", "#72d7f6", "#ff8e95"),
		),
	)
	delivery := barChart(
		[]string{"Sent", "Failed", "Pending"},
		dataset(
			"Delivery",
			numbers(run.MessagesSent, run.MessagesFailed, pending),
			colors("#6de2b7", "#ff8e95", "#6b93f7"),
		),
	)
	httpStatus := barChart(
		[]string{"2xx", "3xx", "4xx", "5xx"},
		dataset(
			"HTTP",
			numbers(run.HTTP2xxCount, run.HTTP3xxCount, run.HTTP4xxCount, run.HTTP5xxCount),
			colors("#6de2b7", "#72d7f6", "#f0be6e", "#ff8e95"),
		),
	)
	latency := barChart(
		[]string{"P50", "P90", "P99", "Max"},
		dataset(
			"Fetch latency (s)",
			seconds(run.FetchLatencyMS),
			color("rgba(111, 225, 183, 0.72)"),
		),
		dataset(
			"Send latency (s)",
			seconds(run.SendLatencyMS),
			color("rgba(126, 167, 255, 0.7)"),
		),
	)
	errorSources := barChart(
		[]string{"Timeout", "Network", "Parse", "Retries", "Rate limit retries"},
		dataset(
			"Counts",
			numbers(
				run.TimeoutCount,
				run.NetworkErrorCount,
				run.ParseErrorCount,
				run.FetchRetriesTotal,
				run.SpecialRateLimitRetries,
			),
			color("rgba(255, 142, 149, 0.72)"),
		),
	)
	memoryChart := barChartPreset(
		labels,
		"bytes",
		dataset("Memory usage", memory, color("rgba(180, 150, 255, 0.72)")),
	)

	return detailCharts{
		FeedHealth:  feedHealth,
		Composition: composition,
		Delivery:    delivery,
		HTTPStatus:  httpStatus,
		Latency:     latency,
		Errors:      errorSources,
		Memory:      memoryChart,
	}
}

func recentRuns(runs []stats.RunSummary) []stats.RunSummary {
	// Detailed charts are bounded even though the run table can
	// load a larger history. This keeps the embedded JSON and chart work small.
	return runs[:min(len(runs), 20)]
}

func dataset(label string, data []float64, background chartColors) chartDataset {
	return chartDataset{
		Label:           label,
		Data:            data,
		BackgroundColor: background,
	}
}

func numbers(values ...int) []float64 {
	result := make([]float64, len(values))
	for i, value := range values {
		result[i] = float64(value)
	}
	return result
}

func seconds(p stats.PercentileStats) []float64 {
	return []float64{
		float64(p.P50) / 1000,
		float64(p.P90) / 1000,
		float64(p.P99) / 1000,
		float64(p.Max) / 1000,
	}
}

func barChart(labels []string, datasets ...chartDataset) string {
	return barChartPreset(labels, "", datasets...)
}

func barChartPreset(labels []string, preset string, datasets ...chartDataset) string {
	return chartJSON(chartSpec{
		Preset: preset,
		Config: chartConfig{
			Type: "bar",
			Data: chartData{
				Labels:   labels,
				Datasets: datasets,
			},
		},
	})
}

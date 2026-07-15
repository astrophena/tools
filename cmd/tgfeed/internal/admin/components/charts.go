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
	RequestPhases string
	Items         string
	Delivery      string
	SendLatency   string
	HTTPStatus    string
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

func detailsCharts(run *stats.Run) detailCharts {
	if run == nil {
		return detailCharts{}
	}
	return detailCharts{
		RequestPhases: requestPhaseChart(run),
		Items:         itemDispositionChart(run),
		Delivery:      deliveryFailureChart(run),
		SendLatency:   deliveryLatencyChart(run),
		HTTPStatus:    httpStatusChart(run),
	}
}

func latencyTrendChart(p StatsProps) string {
	runs := recentRuns(p.Runs)
	labels := make([]string, 0, len(runs))
	p50 := make([]float64, 0, len(runs))
	p90 := make([]float64, 0, len(runs))
	p99 := make([]float64, 0, len(runs))
	times := make([]time.Time, 0, len(runs))
	urls := make([]string, 0, len(runs))
	for i := len(runs) - 1; i >= 0; i-- {
		summary := runs[i]
		labels = append(labels, summary.StartTime.UTC().Format("02.01 15:04"))
		p50 = append(p50, float64(summary.FetchLatencyMS.P50)/1000)
		p90 = append(p90, float64(summary.FetchLatencyMS.P90)/1000)
		p99 = append(p99, float64(summary.FetchLatencyMS.P99)/1000)
		times = append(times, summary.StartTime)
		urls = append(urls, statsURL(summary.StartedAtUnix, p.AutoRefresh, p.DetailsOpen))
	}
	return chartJSON(chartSpec{
		Preset:    "seconds",
		Times:     times,
		SelectURL: urls,
		Config: chartConfig{
			Type: "line",
			Data: chartData{
				Labels: labels,
				Datasets: []chartDataset{
					lineDataset("Fetch p50", p50, "#6fe1b7"),
					lineDataset("Fetch p90", p90, "#72d7f6"),
					lineDataset("Fetch p99", p99, "#ff8e95"),
				},
			},
			Options: chartOptions{Interaction: &chartInteraction{Mode: "index"}},
		},
	})
}

func requestPhaseChart(run *stats.Run) string {
	phases := []stats.DurationStats{
		run.RequestTiming.DNS,
		run.RequestTiming.TCPConnect,
		run.RequestTiming.TLSHandshake,
		run.RequestTiming.RequestWrite,
		run.RequestTiming.ResponseWait,
		run.RequestTiming.ResponseBodyRead,
	}
	values := make([]float64, len(phases))
	hasData := false
	for i, phase := range phases {
		values[i] = float64(phase.PercentileMS.P90) / 1000
		hasData = hasData || phase.Count > 0
	}
	if !hasData {
		return ""
	}
	return barChartPreset(
		[]string{"DNS", "TCP", "TLS", "Request write", "Response wait", "Body read"},
		"seconds",
		dataset("P90", values, color("rgba(111, 225, 183, 0.72)")),
	)
}

func itemDispositionChart(run *stats.Run) string {
	if run.ItemsEnqueuedTotal+run.ItemsDedupedTotal+run.ItemsSkippedOldTotal+run.ItemsFilteredTotal == 0 {
		return ""
	}
	return barChart(
		[]string{"Enqueued", "Deduplicated", "Skipped old", "Filtered"},
		dataset(
			"Items",
			numbers(run.ItemsEnqueuedTotal, run.ItemsDedupedTotal, run.ItemsSkippedOldTotal, run.ItemsFilteredTotal),
			colors("#6de2b7", "#72d7f6", "#f0be6e", "#8d9aa3"),
		),
	)
}

func deliveryFailureChart(run *stats.Run) string {
	if run.MessagesSent+run.MessagesFailed+run.MessagesFormattingFailed == 0 {
		return ""
	}
	return barChart(
		[]string{"Sent", "Send failed", "Formatting failed"},
		dataset(
			"Messages",
			numbers(run.MessagesSent, run.MessagesFailed, run.MessagesFormattingFailed),
			colors("#6de2b7", "#ff8e95", "#f0be6e"),
		),
	)
}

func deliveryLatencyChart(run *stats.Run) string {
	if run.MessagesAttempted == 0 {
		return ""
	}
	return barChartPreset(
		[]string{"P50", "P90", "P99", "Max"},
		"seconds",
		dataset("Telegram delivery", percentileSeconds(run.SendLatencyMS), color("rgba(126, 167, 255, 0.72)")),
	)
}

func httpStatusChart(run *stats.Run) string {
	if run.HTTP4xxCount+run.HTTP5xxCount == 0 {
		return ""
	}
	return barChart(
		[]string{"2xx", "3xx", "4xx", "5xx"},
		dataset(
			"HTTP",
			numbers(run.HTTP2xxCount, run.HTTP3xxCount, run.HTTP4xxCount, run.HTTP5xxCount),
			colors("#6de2b7", "#72d7f6", "#f0be6e", "#ff8e95"),
		),
	)
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

func lineDataset(label string, data []float64, color string) chartDataset {
	return chartDataset{
		Label:           label,
		Data:            data,
		BackgroundColor: chartColors{color},
		BorderColor:     color,
		Tension:         0.32,
	}
}

func numbers(values ...int) []float64 {
	result := make([]float64, len(values))
	for i, value := range values {
		result[i] = float64(value)
	}
	return result
}

func percentileSeconds(values stats.PercentileStats) []float64 {
	return []float64{
		float64(values.P50) / 1000,
		float64(values.P90) / 1000,
		float64(values.P99) / 1000,
		float64(values.Max) / 1000,
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

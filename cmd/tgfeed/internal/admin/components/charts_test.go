// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package components

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.astrophena.name/tools/cmd/tgfeed/internal/stats"
)

func TestChartJSON(t *testing.T) {
	timelineRun := stats.RunSummary{
		StartedAtUnix:    42,
		StartTime:        time.Unix(42, 0).UTC(),
		TotalFeeds:       4,
		SuccessFeeds:     3,
		NotModifiedFeeds: 1,
		FetchLatencyMS: stats.PercentileStats{
			P50: 1000,
			P90: 2000,
			P99: 3000,
		},
	}
	activeRun := &stats.Run{
		RequestTiming: stats.RequestTimingStats{
			DNS: stats.DurationStats{
				Count:        1,
				PercentileMS: stats.PercentileStats{P90: 125},
			},
		},
		ItemsEnqueuedTotal:       3,
		ItemsDedupedTotal:        4,
		ItemsSkippedOldTotal:     5,
		ItemsFilteredTotal:       6,
		MessagesSent:             7,
		MessagesAttempted:        8,
		MessagesFailed:           1,
		MessagesFormattingFailed: 2,
		SendLatencyMS: stats.PercentileStats{
			P50: 100,
			P90: 200,
			P99: 300,
			Max: 400,
		},
		HTTP2xxCount: 8,
		HTTP4xxCount: 1,
	}
	cases := map[string]struct {
		chart string
		want  []string
	}{
		"latency trend": {
			chart: latencyTrendChart(StatsProps{
				Runs: []stats.RunSummary{timelineRun},
			}),
			want: []string{
				`"preset":"seconds"`,
				`"label":"Fetch p50","data":[1]`,
				`"label":"Fetch p99","data":[3]`,
				`"select_urls":["/stats?started_at_unix=42"]`,
			},
		},
		"request phases": {
			chart: requestPhaseChart(activeRun),
			want: []string{
				`"labels":["DNS","TCP","TLS","Request write","Response wait","Body read"]`,
				`"data":[0.125,0,0,0,0,0]`,
			},
		},
		"item disposition": {
			chart: itemDispositionChart(activeRun),
			want: []string{
				`"labels":["Enqueued","Deduplicated","Skipped old","Filtered"]`,
				`"data":[3,4,5,6]`,
			},
		},
		"delivery breakdown": {
			chart: deliveryFailureChart(activeRun),
			want: []string{
				`"labels":["Sent","Send failed","Formatting failed"]`,
				`"data":[7,1,2]`,
			},
		},
		"delivery latency": {
			chart: deliveryLatencyChart(activeRun),
			want: []string{
				`"preset":"seconds"`,
				`"labels":["P50","P90","P99","Max"]`,
				`"data":[0.1,0.2,0.3,0.4]`,
			},
		},
		"http status": {
			chart: httpStatusChart(activeRun),
			want: []string{
				`"labels":["2xx","3xx","4xx","5xx"]`,
				`"data":[8,0,1,0]`,
			},
		},
		"palette": {
			chart: barChart(
				[]string{"good", "bad"},
				dataset("Feeds", numbers(3, 1), colors("green", "red")),
			),
			want: []string{
				`"type":"bar"`,
				`"backgroundColor":["green","red"]`,
			},
		},
		"single color": {
			chart: barChart(
				[]string{"feeds"},
				dataset("Feeds", numbers(3), color("green")),
			),
			want: []string{
				`"backgroundColor":"green"`,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if !json.Valid([]byte(tc.chart)) {
				t.Fatalf("chart JSON is invalid: %s", tc.chart)
			}
			for _, want := range tc.want {
				if !strings.Contains(tc.chart, want) {
					t.Errorf("chart JSON %s does not contain %s", tc.chart, want)
				}
			}
		})
	}
}

func TestConditionalCharts(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		chart string
	}{
		"request phases":   {chart: requestPhaseChart(&stats.Run{})},
		"item disposition": {chart: itemDispositionChart(&stats.Run{})},
		"delivery":         {chart: deliveryFailureChart(&stats.Run{})},
		"delivery latency": {chart: deliveryLatencyChart(&stats.Run{})},
		"http status":      {chart: httpStatusChart(&stats.Run{})},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if tc.chart != "" {
				t.Errorf("chart = %q, want empty", tc.chart)
			}
		})
	}
}

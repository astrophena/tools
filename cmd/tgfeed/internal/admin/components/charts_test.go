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
	}
	cases := map[string]struct {
		chart string
		want  []string
	}{
		"timeline": {
			chart: timelineChart(StatsProps{
				Runs: []stats.RunSummary{timelineRun},
			}),
			want: []string{
				`"type":"line"`,
				`"data":[100]`,
				`"select_urls":["/stats?started_at_unix=42"]`,
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

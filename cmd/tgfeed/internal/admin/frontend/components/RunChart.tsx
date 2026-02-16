import React, { useMemo, useRef } from "npm:react";
import {
  ArcElement,
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  ChartData,
  ChartEvent,
  ChartOptions,
  DoughnutController,
  Filler,
  Legend,
  LineElement,
  LinearScale,
  PointElement,
  Tooltip,
} from "npm:chart.js";
import { Bar, Doughnut, Line, getElementAtEvent } from "npm:react-chartjs-2";

import { formatDateTime, formatDuration } from "../format.ts";
import { StatsRun } from "../types.ts";

ChartJS.register(
  ArcElement,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  BarElement,
  DoughnutController,
  Filler,
  Tooltip,
  Legend,
);

/**
 * Formats chart X-axis labels as day.month and 24-hour time.
 */
function formatRunLabel(value: string | undefined): string {
  if (!value) {
    return "unknown";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return `${date.getDate().toString().padStart(2, "0")}.${(date.getMonth() + 1).toString().padStart(2, "0")} ${date.getHours().toString().padStart(2, "0")}:${date.getMinutes().toString().padStart(2, "0")}`;
}

/**
 * Converts nanoseconds into seconds for chart display.
 */
function toSeconds(valueNs: number | undefined): number {
  return (valueNs ?? 0) / 1_000_000_000;
}

/**
 * Converts milliseconds into seconds for chart display.
 */
function msToSeconds(valueMs: number | undefined): number {
  return (valueMs ?? 0) / 1000;
}

/**
 * Renders interactive stats charts synchronized with selected run details.
 */
export function RunChart(props: {
  stats: StatsRun[];
  selectedStatsIndex: number;
  setSelectedStatsIndex: (index: number) => void;
}) {
  const { stats, selectedStatsIndex, setSelectedStatsIndex } = props;
  const lineChartRef = useRef<ChartJS<"line">>(null);

  const recentRuns = useMemo(() => stats.slice(0, 20).reverse(), [stats]);
  const selectedRun = stats[selectedStatsIndex] ?? stats[0];

  const timelineLabels = useMemo(() => recentRuns.map((run) => formatRunLabel(run.start_time)), [recentRuns]);

  const trendData = useMemo<ChartData<"line">>(() => ({
    labels: timelineLabels,
    datasets: [
      {
        label: "Duration (s)",
        data: recentRuns.map((run) => toSeconds(run.duration)),
        borderColor: "#6fe1b7",
        backgroundColor: "rgba(111, 225, 183, 0.18)",
        yAxisID: "yDuration",
        tension: 0.35,
        fill: true,
        pointRadius: 3,
        pointHoverRadius: 6,
      },
      {
        label: "Messages sent",
        data: recentRuns.map((run) => run.messages_sent ?? 0),
        borderColor: "#7ea7ff",
        backgroundColor: "rgba(126, 167, 255, 0.15)",
        yAxisID: "yCount",
        tension: 0.35,
        borderDash: [5, 4],
        fill: false,
        pointRadius: 3,
        pointHoverRadius: 6,
      },
      {
        label: "Failed feeds",
        data: recentRuns.map((run) => run.failed_feeds ?? 0),
        borderColor: "#ff8a94",
        backgroundColor: "rgba(255, 138, 148, 0.12)",
        yAxisID: "yCount",
        tension: 0.35,
        fill: false,
        pointRadius: 3,
        pointHoverRadius: 6,
      },
    ],
  }), [recentRuns, timelineLabels]);

  const trendOptions = useMemo<ChartOptions<"line">>(() => ({
    responsive: true,
    maintainAspectRatio: false,
    interaction: {
      mode: "index",
      intersect: false,
    },
    plugins: {
      legend: {
        labels: {
          color: "#dbe8ef",
          usePointStyle: true,
          boxWidth: 14,
        },
      },
      tooltip: {
        callbacks: {
          title: (items) => {
            const index = items[0]?.dataIndex ?? 0;
            return formatDateTime(recentRuns[index]?.start_time);
          },
          label: (item) => {
            if (item.dataset.yAxisID === "yDuration") {
              return `Duration: ${Number(item.parsed.y).toFixed(1)} s`;
            }
            return `${item.dataset.label}: ${Number(item.parsed.y).toFixed(0)}`;
          },
        },
      },
    },
    scales: {
      x: {
        ticks: {
          color: "#9db1be",
          maxRotation: 0,
          autoSkip: true,
          maxTicksLimit: 8,
        },
        grid: { color: "rgba(157, 177, 190, 0.08)" },
      },
      yDuration: {
        position: "left",
        beginAtZero: true,
        ticks: {
          color: "#9db1be",
          callback: (value) => `${value} s`,
        },
        grid: { color: "rgba(157, 177, 190, 0.14)" },
      },
      yCount: {
        position: "right",
        beginAtZero: true,
        ticks: {
          color: "#9db1be",
          precision: 0,
        },
        grid: { drawOnChartArea: false },
      },
    },
    onClick: (event: ChartEvent) => {
      if (!lineChartRef.current || !event.native) {
        return;
      }
      const points = getElementAtEvent(lineChartRef.current, {
        nativeEvent: event.native,
      } as React.MouseEvent<HTMLCanvasElement>);
      if (points.length === 0) {
        return;
      }
      const indexFromRecentRuns = points[0]?.index ?? 0;
      const indexFromLatestRuns = stats.length - 1 - indexFromRecentRuns;
      setSelectedStatsIndex(indexFromLatestRuns);
    },
  }), [recentRuns, setSelectedStatsIndex, stats.length]);

  const feedOutcomeData = useMemo<ChartData<"bar">>(() => ({
    labels: timelineLabels,
    datasets: [
      {
        label: "Success",
        data: recentRuns.map((run) => run.success_feeds ?? 0),
        backgroundColor: "rgba(122, 223, 172, 0.78)",
        borderRadius: 6,
      },
      {
        label: "Not modified",
        data: recentRuns.map((run) => run.not_modified_feeds ?? 0),
        backgroundColor: "rgba(228, 164, 77, 0.78)",
        borderRadius: 6,
      },
      {
        label: "Failed",
        data: recentRuns.map((run) => run.failed_feeds ?? 0),
        backgroundColor: "rgba(255, 127, 136, 0.82)",
        borderRadius: 6,
      },
    ],
  }), [recentRuns, timelineLabels]);

  const feedOutcomeOptions = useMemo<ChartOptions<"bar">>(() => ({
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: {
        labels: {
          color: "#dbe8ef",
          usePointStyle: true,
          boxWidth: 14,
        },
      },
      tooltip: {
        callbacks: {
          title: (items) => {
            const index = items[0]?.dataIndex ?? 0;
            return formatDateTime(recentRuns[index]?.start_time);
          },
          footer: (items) => {
            const total = items.reduce((sum, item) => sum + Number(item.parsed.y ?? 0), 0);
            const failed = Number(items.find((item) => item.datasetIndex === 2)?.parsed.y ?? 0);
            const failureRate = total > 0 ? (failed / total) * 100 : 0;
            return `Total feeds: ${total} · Failure rate: ${failureRate.toFixed(1)}%`;
          },
        },
      },
    },
    scales: {
      x: {
        stacked: true,
        ticks: {
          color: "#9db1be",
          maxRotation: 0,
          autoSkip: true,
          maxTicksLimit: 8,
        },
        grid: { color: "rgba(157, 177, 190, 0.08)" },
      },
      y: {
        stacked: true,
        beginAtZero: true,
        ticks: { color: "#9db1be", precision: 0 },
        grid: { color: "rgba(157, 177, 190, 0.14)" },
      },
    },
  }), [recentRuns]);

  const selectedRunDeliveryData = useMemo<ChartData<"doughnut">>(() => {
    const sent = selectedRun?.messages_sent ?? 0;
    const failed = selectedRun?.messages_failed ?? 0;
    const attempted = selectedRun?.messages_attempted ?? 0;
    const pending = Math.max(attempted - sent - failed, 0);
    return {
      labels: ["Sent", "Failed", "Pending"],
      datasets: [{
        data: [sent, failed, pending],
        backgroundColor: ["#6de2b7", "#ff8e95", "#6b93f7"],
        borderColor: "rgba(7, 14, 18, 0.75)",
        borderWidth: 2,
      }],
    };
  }, [selectedRun]);

  const selectedRunHTTPData = useMemo<ChartData<"doughnut">>(() => ({
    labels: ["2xx", "3xx", "4xx", "5xx"],
    datasets: [{
      data: [
        selectedRun?.http_2xx_count ?? 0,
        selectedRun?.http_3xx_count ?? 0,
        selectedRun?.http_4xx_count ?? 0,
        selectedRun?.http_5xx_count ?? 0,
      ],
      backgroundColor: ["#6de2b7", "#72d7f6", "#f0be6e", "#ff8e95"],
      borderColor: "rgba(7, 14, 18, 0.75)",
      borderWidth: 2,
    }],
  }), [selectedRun]);

  const doughnutOptions = useMemo<ChartOptions<"doughnut">>(() => ({
    responsive: true,
    maintainAspectRatio: false,
    cutout: "62%",
    plugins: {
      legend: {
        position: "bottom",
        labels: {
          color: "#dbe8ef",
          padding: 12,
          usePointStyle: true,
          boxWidth: 10,
        },
      },
      tooltip: {
        callbacks: {
          label: (item) => `${item.label}: ${Number(item.raw ?? 0)}`,
        },
      },
    },
  }), []);

  const itemFlowData = useMemo<ChartData<"bar">>(() => ({
    labels: ["Seen", "Kept", "Deduped", "Skipped old", "Enqueued"],
    datasets: [{
      label: "Items",
      data: [
        selectedRun?.items_seen_total ?? 0,
        selectedRun?.items_kept_total ?? 0,
        selectedRun?.items_deduped_total ?? 0,
        selectedRun?.items_skipped_old_total ?? 0,
        selectedRun?.items_enqueued_total ?? 0,
      ],
      backgroundColor: ["#7ea7ff", "#6de2b7", "#f0be6e", "#c994ff", "#72d7f6"],
      borderRadius: 7,
    }],
  }), [selectedRun]);

  const latencyData = useMemo<ChartData<"bar">>(() => ({
    labels: ["P50", "P90", "P99", "Max"],
    datasets: [
      {
        label: "Fetch latency (s)",
        data: [
          msToSeconds(selectedRun?.fetch_latency_ms?.p50),
          msToSeconds(selectedRun?.fetch_latency_ms?.p90),
          msToSeconds(selectedRun?.fetch_latency_ms?.p99),
          msToSeconds(selectedRun?.fetch_latency_ms?.max),
        ],
        backgroundColor: "rgba(111, 225, 183, 0.72)",
        borderRadius: 6,
      },
      {
        label: "Send latency (s)",
        data: [
          msToSeconds(selectedRun?.send_latency_ms?.p50),
          msToSeconds(selectedRun?.send_latency_ms?.p90),
          msToSeconds(selectedRun?.send_latency_ms?.p99),
          msToSeconds(selectedRun?.send_latency_ms?.max),
        ],
        backgroundColor: "rgba(126, 167, 255, 0.7)",
        borderRadius: 6,
      },
    ],
  }), [selectedRun]);

  const errorData = useMemo<ChartData<"bar">>(() => ({
    labels: ["Timeout", "Network", "Parse", "Retries", "Rate limit retries"],
    datasets: [{
      label: "Counts",
      data: [
        selectedRun?.timeout_count ?? 0,
        selectedRun?.network_error_count ?? 0,
        selectedRun?.parse_error_count ?? 0,
        selectedRun?.fetch_retries_total ?? 0,
        selectedRun?.special_rate_limit_retries ?? 0,
      ],
      backgroundColor: "rgba(255, 142, 149, 0.72)",
      borderRadius: 7,
    }],
  }), [selectedRun]);

  const compactBarOptions = useMemo<ChartOptions<"bar">>(() => ({
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: {
        labels: { color: "#dbe8ef", usePointStyle: true, boxWidth: 14 },
      },
    },
    scales: {
      x: {
        ticks: { color: "#9db1be", maxRotation: 0, autoSkip: false },
        grid: { color: "rgba(157, 177, 190, 0.08)" },
      },
      y: {
        beginAtZero: true,
        ticks: { color: "#9db1be", precision: 0 },
        grid: { color: "rgba(157, 177, 190, 0.14)" },
      },
    },
  }), []);

  const selectedRunFeedSuccess = useMemo(() => {
    const total = selectedRun?.total_feeds ?? 0;
    if (total === 0) {
      return "n/a";
    }
    return `${(((selectedRun?.success_feeds ?? 0) / total) * 100).toFixed(1)}%`;
  }, [selectedRun]);

  return (
    <section className="panel chart-panel">
      <header className="panel-header">
        <div>
          <h2>Interactive Run Analytics</h2>
          <p>Click any point in the timeline chart to inspect that run and keep all analytics blocks in sync.</p>
        </div>
      </header>

      {selectedRun && (
        <div className="chart-kpis">
          <span className="chart-kpi"><b>Selected run</b>{formatDateTime(selectedRun.start_time)}</span>
          <span className="chart-kpi"><b>Total feeds</b>{selectedRun.total_feeds ?? 0}</span>
          <span className="chart-kpi"><b>Failed feeds</b>{selectedRun.failed_feeds ?? 0}</span>
          <span className="chart-kpi"><b>Feed success</b>{selectedRunFeedSuccess}</span>
          <span className="chart-kpi"><b>Run duration</b>{formatDuration(selectedRun.duration)}</span>
        </div>
      )}

      {recentRuns.length === 0 && <p className="message message-info">No chart data yet.</p>}
      {recentRuns.length > 0 && (
        <div className="chart-stack">
          <article className="chart-card">
            <h3>Timeline</h3>
            <p className="chart-note">Duration in seconds, messages sent, and failed feeds over recent runs.</p>
            <div className="chart-canvas chart-canvas-lg">
              <Line ref={lineChartRef} data={trendData} options={trendOptions} />
            </div>
          </article>

          <article className="chart-card">
            <h3>Feed outcomes by run</h3>
            <p className="chart-note">Stacked breakdown of success, not-modified, and failed feeds.</p>
            <div className="chart-canvas">
              <Bar data={feedOutcomeData} options={feedOutcomeOptions} />
            </div>
          </article>

          <div className="chart-matrix">
            <article className="chart-card">
              <h3>Delivery outcome</h3>
              <p className="chart-note">Attempted messages split by sent, failed, and pending.</p>
              <div className="chart-canvas">
                <Doughnut data={selectedRunDeliveryData} options={doughnutOptions} />
              </div>
            </article>

            <article className="chart-card">
              <h3>HTTP status classes</h3>
              <p className="chart-note">Request result classes from the selected run.</p>
              <div className="chart-canvas">
                <Doughnut data={selectedRunHTTPData} options={doughnutOptions} />
              </div>
            </article>

            <article className="chart-card">
              <h3>Item pipeline</h3>
              <p className="chart-note">How fetched items moved through filtering and enqueue stages.</p>
              <div className="chart-canvas">
                <Bar data={itemFlowData} options={compactBarOptions} />
              </div>
            </article>

            <article className="chart-card">
              <h3>Latency percentiles</h3>
              <p className="chart-note">Fetch and send latency percentiles in seconds.</p>
              <div className="chart-canvas">
                <Bar data={latencyData} options={compactBarOptions} />
              </div>
            </article>

            <article className="chart-card chart-card-wide">
              <h3>Error and retry sources</h3>
              <p className="chart-note">Timeout, network and parse errors, plus retry pressure indicators.</p>
              <div className="chart-canvas">
                <Bar data={errorData} options={compactBarOptions} />
              </div>
            </article>
          </div>
        </div>
      )}
    </section>
  );
}

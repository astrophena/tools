import React, { useMemo, useRef } from "react";
import {
  CategoryScale,
  Chart as ChartJS,
  ChartData,
  ChartEvent,
  ChartOptions,
  Filler,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
} from "chart.js";
import { Line } from "react-chartjs-2";

import { formatDateTime } from "../format.ts";
import { StatsRun } from "../types.ts";

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
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
  return `${date.getDate().toString().padStart(2, "0")}.${
    (date.getMonth() + 1).toString().padStart(2, "0")
  } ${date.getHours().toString().padStart(2, "0")}:${
    date.getMinutes().toString().padStart(2, "0")
  }`;
}

/**
 * Converts nanoseconds into seconds for chart display.
 */
function toSeconds(valueNs: number | undefined): number {
  return (valueNs ?? 0) / 1_000_000_000;
}

export function TimelineChart(props: {
  stats: StatsRun[];
  setSelectedStatsIndex: (index: number) => void;
}) {
  const { stats, setSelectedStatsIndex } = props;
  const lineChartRef = useRef<ChartJS<"line">>(null);

  // Take top 20 runs for the timeline, reversed so time goes left to right.
  const recentRuns = useMemo(() => stats.slice(0, 20).reverse(), [stats]);
  const timelineLabels = useMemo(
    () => recentRuns.map((run) => formatRunLabel(run.start_time)),
    [recentRuns],
  );

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
      if (!lineChartRef.current) {
        return;
      }
      const points = lineChartRef.current.getElementsAtEventForMode(
        event,
        "nearest",
        { intersect: true },
        false,
      );
      if (points.length === 0) {
        return;
      }
      const indexFromRecentRuns = points[0]?.index ?? 0;
      const indexFromLatestRuns = recentRuns.length - 1 - indexFromRecentRuns;
      setSelectedStatsIndex(indexFromLatestRuns);
    },
  }), [recentRuns, setSelectedStatsIndex]);

  return (
    <article className="chart-card">
      <h3>Timeline</h3>
      <p className="chart-note">
        Duration in seconds, messages sent, and failed feeds over recent runs.
      </p>
      <div className="chart-canvas chart-canvas-lg">
        <Line ref={lineChartRef} data={trendData} options={trendOptions} />
      </div>
    </article>
  );
}

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

/** Formats chart X-axis labels as day.month and 24-hour time. */
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

function toNumber(value: number | undefined): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return 0;
  }
  return value;
}

function healthyFeeds(run: StatsRun): number {
  return toNumber(run.success_feeds) + toNumber(run.not_modified_feeds);
}

function healthyRate(run: StatsRun): number {
  const total = toNumber(run.total_feeds);
  if (total <= 0) {
    return 0;
  }
  return (healthyFeeds(run) / total) * 100;
}

export const TimelineChart = React.memo(function TimelineChart(props: {
  stats: StatsRun[];
  selectedStatsIndex: number;
  onSelectRun: (index: number) => void;
}) {
  const { stats, selectedStatsIndex, onSelectRun } = props;
  const lineChartRef = useRef<ChartJS<"line">>(null);

  const recentRuns = useMemo(() => stats.slice(0, 20).reverse(), [stats]);
  const timelineLabels = useMemo(
    () => recentRuns.map((run) => formatRunLabel(run.start_time)),
    [recentRuns],
  );

  const selectedRecentIndex = useMemo(() => {
    const idx = recentRuns.length - 1 - selectedStatsIndex;
    if (idx < 0 || idx >= recentRuns.length) {
      return -1;
    }
    return idx;
  }, [recentRuns.length, selectedStatsIndex]);

  const trendData = useMemo<ChartData<"line">>(() => ({
    labels: timelineLabels,
    datasets: [
      {
        label: "Healthy feeds (%)",
        data: recentRuns.map((run) => healthyRate(run)),
        borderColor: "#6fe1b7",
        backgroundColor: "rgba(111, 225, 183, 0.2)",
        tension: 0.32,
        fill: true,
        pointRadius: recentRuns.map((_, index) =>
          index === selectedRecentIndex ? 5 : 3
        ),
        pointHoverRadius: 6,
        pointBackgroundColor: recentRuns.map((_, index) =>
          index === selectedRecentIndex ? "#fff" : "#6fe1b7"
        ),
      },
    ],
  }), [recentRuns, selectedRecentIndex, timelineLabels]);

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
            const index = item.dataIndex;
            const run = recentRuns[index];
            if (!run) {
              return "Healthy feeds: n/a";
            }
            const healthy = healthyFeeds(run);
            const total = toNumber(run?.total_feeds);
            return `Healthy feeds: ${healthy}/${total} (${
              Number(item.parsed.y).toFixed(1)
            }%)`;
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
      y: {
        beginAtZero: true,
        max: 100,
        ticks: {
          color: "#9db1be",
          callback: (value) => `${value}%`,
        },
        grid: { color: "rgba(157, 177, 190, 0.14)" },
      },
    },
    onClick: (event: ChartEvent) => {
      if (!lineChartRef.current) {
        return;
      }
      const nativeEvent = event.native;
      if (!nativeEvent) {
        return;
      }
      const points = lineChartRef.current.getElementsAtEventForMode(
        nativeEvent,
        "nearest",
        { intersect: true },
        false,
      );
      if (points.length === 0) {
        return;
      }
      const indexFromRecentRuns = points[0]?.index ?? 0;
      const indexFromLatestRuns = recentRuns.length - 1 - indexFromRecentRuns;
      onSelectRun(indexFromLatestRuns);
    },
  }), [onSelectRun, recentRuns]);

  return (
    <article className="chart-card">
      <h3>Health Trend</h3>
      <p className="chart-note">
        Healthy feed rate across recent runs. Not changed feeds count as
        healthy.
      </p>
      <div className="chart-canvas chart-canvas-lg">
        <Line ref={lineChartRef} data={trendData} options={trendOptions} />
      </div>
    </article>
  );
});

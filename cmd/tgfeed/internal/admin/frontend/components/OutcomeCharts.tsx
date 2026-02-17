import React, { useMemo } from "react";
import {
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  ChartData,
  ChartOptions,
  Legend,
  LinearScale,
  Tooltip,
} from "chart.js";
import { Bar } from "react-chartjs-2";

import { formatDateTime } from "../format.ts";
import { StatsRun } from "../types.ts";

ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  Tooltip,
  Legend,
);

function toNumber(value: number | undefined): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return 0;
  }
  return value;
}

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

function healthyFeeds(run: StatsRun): number {
  return toNumber(run.success_feeds) + toNumber(run.not_modified_feeds);
}

function feedRate(part: number, total: number): number {
  if (total <= 0) {
    return 0;
  }
  return (part / total) * 100;
}

export function OutcomeCharts(props: {
  stats: StatsRun[];
  activeRun: StatsRun | undefined;
}) {
  const { stats, activeRun } = props;
  const recentRuns = useMemo(() => stats.slice(0, 20).reverse(), [stats]);
  const timelineLabels = useMemo(
    () => recentRuns.map((run) => formatRunLabel(run.start_time)),
    [recentRuns],
  );

  const feedOutcomeData = useMemo<ChartData<"bar">>(() => ({
    labels: timelineLabels,
    datasets: [
      {
        label: "Healthy",
        data: recentRuns.map((run) => healthyFeeds(run)),
        backgroundColor: "rgba(122, 223, 172, 0.8)",
        borderRadius: 6,
      },
      {
        label: "Failed",
        data: recentRuns.map((run) => toNumber(run.failed_feeds)),
        backgroundColor: "rgba(255, 127, 136, 0.85)",
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
            const healthy = Number(
              items.find((item) => item.datasetIndex === 0)?.parsed.y ?? 0,
            );
            const failed = Number(
              items.find((item) => item.datasetIndex === 1)?.parsed.y ?? 0,
            );
            const total = healthy + failed;
            return `Healthy rate: ${feedRate(healthy, total).toFixed(1)}%`;
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

  const selectedRunBreakdown = useMemo<ChartData<"bar">>(() => ({
    labels: ["Success", "Not changed", "Failed"],
    datasets: [{
      label: "Feeds",
      data: [
        toNumber(activeRun?.success_feeds),
        toNumber(activeRun?.not_modified_feeds),
        toNumber(activeRun?.failed_feeds),
      ],
      backgroundColor: ["#6de2b7", "#72d7f6", "#ff8e95"],
      borderRadius: 7,
    }],
  }), [activeRun]);

  const selectedTotalFeeds = toNumber(activeRun?.success_feeds) +
    toNumber(activeRun?.not_modified_feeds) +
    toNumber(activeRun?.failed_feeds);

  const compactBarOptions = useMemo<ChartOptions<"bar">>(() => ({
    responsive: true,
    maintainAspectRatio: false,
    indexAxis: "y",
    plugins: {
      legend: {
        display: false,
      },
      tooltip: {
        callbacks: {
          label: (item) => {
            const count = Number(item.raw ?? 0);
            return `${item.label}: ${count} (${
              feedRate(count, selectedTotalFeeds).toFixed(1)
            }%)`;
          },
        },
      },
    },
    scales: {
      x: {
        beginAtZero: true,
        ticks: { color: "#9db1be", precision: 0 },
        grid: { color: "rgba(157, 177, 190, 0.08)" },
      },
      y: {
        ticks: { color: "#9db1be", maxRotation: 0, autoSkip: false },
        grid: { display: false },
      },
    },
  }), [selectedTotalFeeds]);

  return (
    <>
      <article className="chart-card">
        <h3>Feed health by run</h3>
        <p className="chart-note">
          Healthy feeds include successful and not changed feeds.
        </p>
        <div className="chart-canvas">
          <Bar data={feedOutcomeData} options={feedOutcomeOptions} />
        </div>
      </article>

      <article className="chart-card">
        <h3>Healthy feed composition</h3>
        <p className="chart-note">
          Active run split into success, not changed, and failed feeds.
        </p>
        <div className="chart-canvas">
          <Bar data={selectedRunBreakdown} options={compactBarOptions} />
        </div>
      </article>
    </>
  );
}

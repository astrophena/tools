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

import { formatBytes, formatDateTime } from "../format.ts";
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

export const MemoryCharts = React.memo(function MemoryCharts(props: {
  stats: StatsRun[];
}) {
  const { stats } = props;
  const recentRuns = useMemo(() => stats.slice(0, 20).reverse(), [stats]);
  const timelineLabels = useMemo(
    () => recentRuns.map((run) => formatRunLabel(run.start_time)),
    [recentRuns],
  );

  const memoryData = useMemo<ChartData<"bar">>(() => ({
    labels: timelineLabels,
    datasets: [
      {
        label: "Memory Usage",
        data: recentRuns.map((run) => toNumber(run.memory_usage)),
        backgroundColor: "rgba(180, 150, 255, 0.72)",
        borderRadius: 6,
      },
    ],
  }), [recentRuns, timelineLabels]);

  const memoryOptions = useMemo<ChartOptions<"bar">>(() => ({
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: {
        display: false,
      },
      tooltip: {
        callbacks: {
          title: (items) => {
            const index = items[0]?.dataIndex ?? 0;
            return formatDateTime(recentRuns[index]?.start_time);
          },
          label: (item) => {
            const count = Number(item.raw ?? 0);
            return `Memory: ${formatBytes(count)}`;
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
        ticks: {
          color: "#9db1be",
          callback: (value) => {
            return formatBytes(Number(value));
          },
        },
        grid: { color: "rgba(157, 177, 190, 0.14)" },
      },
    },
  }), [recentRuns]);

  return (
    <article className="chart-card">
      <h3>Memory usage by run</h3>
      <p className="chart-note">
        Total allocated memory usage at the end of the run.
      </p>
      <div className="chart-canvas">
        <Bar data={memoryData} options={memoryOptions} />
      </div>
    </article>
  );
});

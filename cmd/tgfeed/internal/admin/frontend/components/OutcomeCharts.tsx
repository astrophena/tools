import React, { useMemo } from "npm:react";
import {
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  ChartData,
  ChartOptions,
  Legend,
  LinearScale,
  Tooltip,
} from "npm:chart.js";
import { Bar } from "npm:react-chartjs-2";

import { formatDateTime } from "../format.ts";
import { StatsRun } from "../types.ts";

ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  Tooltip,
  Legend,
);

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

export function OutcomeCharts(props: {
  stats: StatsRun[];
  selectedRun: StatsRun | undefined;
}) {
  const { stats, selectedRun } = props;
  const recentRuns = useMemo(() => stats.slice(0, 20).reverse(), [stats]);
  const timelineLabels = useMemo(() => recentRuns.map((run) => formatRunLabel(run.start_time)), [recentRuns]);

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

  const compactBarOptions = useMemo<ChartOptions<"bar">>(() => ({
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: {
        display: false,
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

  return (
    <>
      <article className="chart-card">
        <h3>Feed outcomes by run</h3>
        <p className="chart-note">Stacked breakdown of success, not-modified, and failed feeds.</p>
        <div className="chart-canvas">
          <Bar data={feedOutcomeData} options={feedOutcomeOptions} />
        </div>
      </article>

      <article className="chart-card">
        <h3>Item pipeline</h3>
        <p className="chart-note">How fetched items moved through filtering and enqueue stages.</p>
        <div className="chart-canvas">
          <Bar data={itemFlowData} options={compactBarOptions} />
        </div>
      </article>
    </>
  );
}

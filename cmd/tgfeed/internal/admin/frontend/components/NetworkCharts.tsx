import React, { useMemo } from "react";
import {
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  ChartData,
  ChartOptions,
  LinearScale,
  Tooltip,
} from "chart.js";
import { Bar } from "react-chartjs-2";

import { StatsRun } from "../types.ts";

ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  Tooltip,
);

type Segment = {
  label: string;
  count: number;
  color: string;
};

function toNumber(value: number | undefined): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return 0;
  }
  return value;
}

function percent(part: number, total: number): number {
  if (total <= 0) {
    return 0;
  }
  return (part / total) * 100;
}

function compactBarData(label: string, segments: Segment[]): ChartData<"bar"> {
  return {
    labels: segments.map((segment) => segment.label),
    datasets: [{
      label,
      data: segments.map((segment) => segment.count),
      backgroundColor: segments.map((segment) => segment.color),
      borderRadius: 7,
    }],
  };
}

function compactBarOptions(
  total: number,
  segments: Segment[],
): ChartOptions<"bar"> {
  return {
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
            const label = item.label ?? "";
            const count = Number(item.raw ?? 0);
            return `${label}: ${count} (${percent(count, total).toFixed(1)}%)`;
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
  };
}

function summaryList(segments: Segment[], total: number) {
  return (
    <ul className="bar-summary">
      {segments.map((segment) => (
        <li key={segment.label}>
          <span>{segment.label}</span>
          <b>{segment.count} ({percent(segment.count, total).toFixed(1)}%)</b>
        </li>
      ))}
    </ul>
  );
}

export function NetworkCharts(props: {
  activeRun: StatsRun | undefined;
}) {
  const { activeRun } = props;

  const deliverySegments = useMemo<Segment[]>(() => {
    const sent = toNumber(activeRun?.messages_sent);
    const failed = toNumber(activeRun?.messages_failed);
    const attempted = toNumber(activeRun?.messages_attempted);
    const pending = Math.max(attempted - sent - failed, 0);
    return [
      { label: "Sent", count: sent, color: "#6de2b7" },
      { label: "Failed", count: failed, color: "#ff8e95" },
      { label: "Pending", count: pending, color: "#6b93f7" },
    ];
  }, [activeRun]);

  const deliveryTotal = useMemo(
    () => deliverySegments.reduce((sum, item) => sum + item.count, 0),
    [deliverySegments],
  );

  const httpSegments = useMemo<Segment[]>(() => [
    {
      label: "2xx",
      count: toNumber(activeRun?.http_2xx_count),
      color: "#6de2b7",
    },
    {
      label: "3xx",
      count: toNumber(activeRun?.http_3xx_count),
      color: "#72d7f6",
    },
    {
      label: "4xx",
      count: toNumber(activeRun?.http_4xx_count),
      color: "#f0be6e",
    },
    {
      label: "5xx",
      count: toNumber(activeRun?.http_5xx_count),
      color: "#ff8e95",
    },
  ], [activeRun]);

  const httpTotal = useMemo(
    () => httpSegments.reduce((sum, item) => sum + item.count, 0),
    [httpSegments],
  );

  return (
    <>
      <article className="chart-card">
        <h3>Delivery outcome</h3>
        <p className="chart-note">
          Counts and percentages for sent, failed, and pending messages.
        </p>
        <div className="chart-canvas chart-canvas-compact">
          <Bar
            data={compactBarData("Delivery", deliverySegments)}
            options={compactBarOptions(deliveryTotal, deliverySegments)}
          />
        </div>
        {summaryList(deliverySegments, deliveryTotal)}
      </article>

      <article className="chart-card">
        <h3>HTTP status classes</h3>
        <p className="chart-note">
          Response class distribution for the active run.
        </p>
        <div className="chart-canvas chart-canvas-compact">
          <Bar
            data={compactBarData("HTTP", httpSegments)}
            options={compactBarOptions(httpTotal, httpSegments)}
          />
        </div>
        {summaryList(httpSegments, httpTotal)}
      </article>
    </>
  );
}

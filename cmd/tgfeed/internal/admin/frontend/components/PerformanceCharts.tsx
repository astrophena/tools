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

import { StatsRun } from "../types.ts";

ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  Tooltip,
  Legend,
);

function msToSeconds(valueMs: number | undefined): number {
  return (valueMs ?? 0) / 1000;
}

export function PerformanceCharts(props: {
  selectedRun: StatsRun | undefined;
}) {
  const { selectedRun } = props;

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

  return (
    <>
      <article className="chart-card">
        <h3>Latency percentiles</h3>
        <p className="chart-note">
          Fetch and send latency percentiles in seconds.
        </p>
        <div className="chart-canvas">
          <Bar data={latencyData} options={compactBarOptions} />
        </div>
      </article>

      <article className="chart-card chart-card-wide">
        <h3>Error and retry sources</h3>
        <p className="chart-note">
          Timeout, network and parse errors, plus retry pressure indicators.
        </p>
        <div className="chart-canvas">
          <Bar data={errorData} options={compactBarOptions} />
        </div>
      </article>
    </>
  );
}

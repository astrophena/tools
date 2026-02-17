import React, { useMemo } from "react";
import {
  ArcElement,
  Chart as ChartJS,
  ChartData,
  ChartOptions,
  DoughnutController,
  Legend,
  Tooltip,
} from "chart.js";
import { Doughnut } from "react-chartjs-2";

import { StatsRun } from "../types.ts";

ChartJS.register(
  ArcElement,
  DoughnutController,
  Tooltip,
  Legend,
);

export function NetworkCharts(props: {
  selectedRun: StatsRun | undefined;
}) {
  const { selectedRun } = props;

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

  return (
    <>
      <article className="chart-card">
        <h3>Delivery outcome</h3>
        <p className="chart-note">
          Attempted messages split by sent, failed, and pending.
        </p>
        <div className="chart-canvas">
          <Doughnut data={selectedRunDeliveryData} options={doughnutOptions} />
        </div>
      </article>

      <article className="chart-card">
        <h3>HTTP status classes</h3>
        <p className="chart-note">
          Request result classes from the selected run.
        </p>
        <div className="chart-canvas">
          <Doughnut data={selectedRunHTTPData} options={doughnutOptions} />
        </div>
      </article>
    </>
  );
}

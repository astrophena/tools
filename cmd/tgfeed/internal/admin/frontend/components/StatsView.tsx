import React, { useMemo, useState } from "react";

import { formatDateTime, formatDuration } from "../format.ts";
import { StatsRun } from "../types.ts";
import { ChartsGrid } from "./ChartsGrid.tsx";
import { NetworkCharts } from "./NetworkCharts.tsx";
import { OutcomeCharts } from "./OutcomeCharts.tsx";
import { PerformanceCharts } from "./PerformanceCharts.tsx";
import { TimelineChart } from "./TimelineChart.tsx";
import { TopFeedsPanels } from "./TopFeedsPanels.tsx";

/** Relative trend quality for run-over-run metric changes. */
type DeltaTone = "good" | "bad" | "neutral";

/** User-selected run context for detailed analytics blocks. */
type RunContextMode = "latest" | "selected";

/** Formats an ISO timestamp into a short relative string. */
function formatRelativeTime(value: string | undefined): string {
  if (!value) {
    return "n/a";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "n/a";
  }
  const diff = Date.now() - date.getTime();
  if (diff < 0) {
    return "just now";
  }
  const sec = Math.floor(diff / 1000);
  if (sec < 60) {
    return `${sec}s ago`;
  }
  const min = Math.floor(sec / 60);
  if (min < 60) {
    return `${min}m ago`;
  }
  const hr = Math.floor(min / 60);
  if (hr < 24) {
    return `${hr}h ago`;
  }
  const day = Math.floor(hr / 24);
  return `${day}d ago`;
}

function toNumber(value: number | undefined): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return 0;
  }
  return value;
}

function percent(part: number, total: number): number | undefined {
  if (total <= 0) {
    return undefined;
  }
  return (part / total) * 100;
}

function formatPercent(value: number | undefined): string {
  if (value === undefined) {
    return "n/a";
  }
  return `${value.toFixed(1)}%`;
}

function healthyFeedCount(run: StatsRun | undefined): number {
  return toNumber(run?.success_feeds) + toNumber(run?.not_modified_feeds);
}

function healthBadge(run: StatsRun | undefined): {
  label: string;
  tone: "healthy" | "degraded" | "failing" | "unknown";
  detail: string;
} {
  if (!run) {
    return {
      label: "No data",
      tone: "unknown",
      detail: "No persisted run snapshots are available yet.",
    };
  }

  const totalFeeds = toNumber(run.total_feeds);
  if (totalFeeds <= 0) {
    return {
      label: "Idle",
      tone: "unknown",
      detail: "Latest run processed zero feeds.",
    };
  }

  const healthyFeeds = healthyFeedCount(run);
  const failedFeeds = toNumber(run.failed_feeds);
  const healthyRate = percent(healthyFeeds, totalFeeds);
  const deliveryRate = percent(
    toNumber(run.messages_sent),
    toNumber(run.messages_attempted),
  );

  if (healthyRate !== undefined && healthyRate >= 98 && failedFeeds === 0) {
    return {
      label: "Healthy",
      tone: "healthy",
      detail: `Healthy feeds: ${healthyFeeds}/${totalFeeds} (${
        formatPercent(healthyRate)
      }) · Delivery: ${formatPercent(deliveryRate)}`,
    };
  }

  if (healthyRate !== undefined && healthyRate >= 90) {
    return {
      label: "Degraded",
      tone: "degraded",
      detail: `Healthy feeds: ${healthyFeeds}/${totalFeeds} (${
        formatPercent(healthyRate)
      }) · Failures: ${failedFeeds}`,
    };
  }

  return {
    label: "Failing",
    tone: "failing",
    detail: `Healthy feeds: ${healthyFeeds}/${totalFeeds} (${
      formatPercent(healthyRate)
    }) · Failures: ${failedFeeds}`,
  };
}

function deltaTone(
  delta: number | undefined,
  betterWhen: "higher" | "lower",
): DeltaTone {
  if (delta === undefined || Math.abs(delta) < 0.0001) {
    return "neutral";
  }
  const isGood = (betterWhen === "higher" && delta > 0) ||
    (betterWhen === "lower" && delta < 0);
  return isGood ? "good" : "bad";
}

function deltaText(
  delta: number | undefined,
  opts: { precision: number; unit: string },
): string {
  if (delta === undefined) {
    return "vs prev run: n/a";
  }
  if (Math.abs(delta) < 0.0001) {
    return "no change vs prev run";
  }
  const sign = delta > 0 ? "+" : "-";
  const abs = Math.abs(delta).toFixed(opts.precision);
  return `${sign}${abs}${opts.unit} vs prev run`;
}

function formatRefreshTime(value: number | null): string {
  if (value === null) {
    return "Not refreshed yet";
  }
  return `Last refreshed: ${formatDateTime(new Date(value).toISOString())}`;
}

/**
 * Renders dashboard health status, run context controls, and analytics details.
 */
export function StatsView(props: {
  stats: StatsRun[];
  statsLoading: boolean;
  statsError: string;
  selectedStatsIndex: number;
  setSelectedStatsIndex: (index: number) => void;
  loadStats: () => Promise<void>;
  lastStatsRefreshedAt: number | null;
  autoRefreshStats: boolean;
  setAutoRefreshStats: (next: boolean) => void;
}) {
  const {
    stats,
    statsLoading,
    statsError,
    selectedStatsIndex,
    setSelectedStatsIndex,
    loadStats,
    lastStatsRefreshedAt,
    autoRefreshStats,
    setAutoRefreshStats,
  } = props;

  const latestStats = stats.length > 0 ? stats[0] : undefined;
  const previousStats = stats.length > 1 ? stats[1] : undefined;
  const selectedStats = stats[selectedStatsIndex] ?? latestStats;

  const [runContextMode, setRunContextMode] = useState<RunContextMode>(
    "latest",
  );

  const activeRun = runContextMode === "selected"
    ? selectedStats ?? latestStats
    : latestStats;

  const runContextLabel =
    runContextMode === "selected" && selectedStatsIndex > 0
      ? "Pinned run"
      : "Latest run";

  const health = useMemo(() => healthBadge(latestStats), [latestStats]);

  const latestTotalFeeds = toNumber(latestStats?.total_feeds);
  const previousTotalFeeds = toNumber(previousStats?.total_feeds);
  const latestHealthyFeeds = healthyFeedCount(latestStats);
  const previousHealthyFeeds = healthyFeedCount(previousStats);

  const latestHealthyRate = percent(latestHealthyFeeds, latestTotalFeeds);
  const previousHealthyRate = previousStats
    ? percent(previousHealthyFeeds, previousTotalFeeds)
    : undefined;
  const healthyRateDelta =
    latestHealthyRate !== undefined && previousHealthyRate !== undefined
      ? latestHealthyRate - previousHealthyRate
      : undefined;

  const latestFailedFeeds = toNumber(latestStats?.failed_feeds);
  const previousFailedFeeds = previousStats
    ? toNumber(previousStats.failed_feeds)
    : undefined;
  const failedDelta = previousFailedFeeds === undefined
    ? undefined
    : latestFailedFeeds - previousFailedFeeds;

  const latestDeliveryRate = percent(
    toNumber(latestStats?.messages_sent),
    toNumber(latestStats?.messages_attempted),
  );
  const previousDeliveryRate = previousStats
    ? percent(
      toNumber(previousStats.messages_sent),
      toNumber(previousStats.messages_attempted),
    )
    : undefined;
  const deliveryRateDelta =
    latestDeliveryRate !== undefined && previousDeliveryRate !== undefined
      ? latestDeliveryRate - previousDeliveryRate
      : undefined;

  const latestP99 = toNumber(latestStats?.fetch_latency_ms?.p99);
  const previousP99 = previousStats
    ? toNumber(previousStats.fetch_latency_ms?.p99)
    : undefined;
  const p99Delta = previousP99 === undefined
    ? undefined
    : latestP99 - previousP99;

  const latestDurationSec = toNumber(latestStats?.duration) / 1_000_000_000;
  const previousDurationSec = previousStats
    ? toNumber(previousStats.duration) / 1_000_000_000
    : undefined;
  const durationDelta = previousDurationSec === undefined
    ? undefined
    : latestDurationSec - previousDurationSec;

  function selectRun(index: number): void {
    setSelectedStatsIndex(index);
    if (index === 0) {
      setRunContextMode("latest");
      return;
    }
    setRunContextMode("selected");
  }

  return (
    <div className="column">
      <section className="panel stats-panel">
        <header className="panel-header">
          <div>
            <h2>System Health</h2>
            <p>
              Triage view from the latest run. Not changed feeds are treated as
              healthy.
            </p>
          </div>
          <div className="panel-header-actions">
            <button
              className="button button-ghost"
              type="button"
              onClick={() => void loadStats()}
              disabled={statsLoading}
            >
              {statsLoading ? "Loading..." : "Refresh stats"}
            </button>
            <label className="auto-refresh-toggle" htmlFor="auto-refresh">
              <input
                id="auto-refresh"
                type="checkbox"
                checked={autoRefreshStats}
                onChange={(event) => {
                  setAutoRefreshStats(event.target.checked);
                }}
              />
              Auto-refresh every 30s
            </label>
          </div>
        </header>

        <div className="system-meta">
          <span>{formatRefreshTime(lastStatsRefreshedAt)}</span>
          <span className={`health-badge health-badge-${health.tone}`}>
            {health.label}
          </span>
        </div>
        <p className="system-detail">{health.detail}</p>

        {statsError && <p className="message message-error">{statsError}</p>}
        {!statsError && stats.length === 0 && !statsLoading && (
          <p className="message message-info">No stats available yet.</p>
        )}

        {latestStats && (
          <div className="indicator-grid">
            <article className="indicator indicator-primary">
              <p>Last run</p>
              <h3>{formatRelativeTime(latestStats.start_time)}</h3>
              <span>{formatDateTime(latestStats.start_time)}</span>
              <span className="delta delta-neutral">Context: latest run</span>
            </article>
            <article className="indicator">
              <p>Healthy feeds</p>
              <h3>{latestHealthyFeeds}/{latestTotalFeeds}</h3>
              <span>Rate: {formatPercent(latestHealthyRate)}</span>
              <span
                className={`delta delta-${
                  deltaTone(healthyRateDelta, "higher")
                }`}
              >
                {deltaText(healthyRateDelta, { precision: 1, unit: " pp" })}
              </span>
            </article>
            <article className="indicator indicator-danger">
              <p>Failed feeds</p>
              <h3>{latestFailedFeeds}</h3>
              <span>From total: {latestTotalFeeds}</span>
              <span
                className={`delta delta-${deltaTone(failedDelta, "lower")}`}
              >
                {deltaText(failedDelta, { precision: 0, unit: "" })}
              </span>
            </article>
            <article className="indicator">
              <p>Delivery success</p>
              <h3>{formatPercent(latestDeliveryRate)}</h3>
              <span>
                Sent {toNumber(latestStats.messages_sent)}/
                {toNumber(latestStats.messages_attempted)}
              </span>
              <span
                className={`delta delta-${
                  deltaTone(deliveryRateDelta, "higher")
                }`}
              >
                {deltaText(deliveryRateDelta, { precision: 1, unit: " pp" })}
              </span>
            </article>
            <article className="indicator">
              <p>Fetch latency p99</p>
              <h3>{latestP99.toFixed(0)} ms</h3>
              <span>Lower is better</span>
              <span className={`delta delta-${deltaTone(p99Delta, "lower")}`}>
                {deltaText(p99Delta, { precision: 0, unit: " ms" })}
              </span>
            </article>
            <article className="indicator">
              <p>Run duration</p>
              <h3>{formatDuration(latestStats.duration)}</h3>
              <span>Lower is better</span>
              <span
                className={`delta delta-${deltaTone(durationDelta, "lower")}`}
              >
                {deltaText(durationDelta, { precision: 1, unit: " s" })}
              </span>
            </article>
          </div>
        )}
      </section>

      <section className="panel chart-panel">
        <header className="panel-header">
          <div>
            <h2>Run Explorer</h2>
            <p>
              Inspect one run in detail, or track latest run continuously.
            </p>
          </div>
        </header>

        {activeRun && (
          <div className="run-context-row">
            <div className="run-context-copy">
              <p>{runContextLabel}</p>
              <h3>{formatDateTime(activeRun.start_time)}</h3>
              <span>{formatRelativeTime(activeRun.start_time)}</span>
            </div>
            <div className="run-context-actions">
              <button
                type="button"
                className={runContextMode === "latest"
                  ? "tab-button active"
                  : "tab-button"}
                onClick={() => {
                  setRunContextMode("latest");
                  setSelectedStatsIndex(0);
                }}
              >
                Track latest
              </button>
              <button
                type="button"
                className={runContextMode === "selected"
                  ? "tab-button active"
                  : "tab-button"}
                onClick={() => {
                  if (selectedStatsIndex === 0 && stats.length > 1) {
                    selectRun(1);
                    return;
                  }
                  setRunContextMode("selected");
                }}
                disabled={stats.length < 2}
              >
                Pin selected run
              </button>
            </div>
          </div>
        )}

        {stats.length === 0 && !statsLoading && (
          <p className="message message-info">No chart data yet.</p>
        )}

        {stats.length > 0 && (
          <div className="chart-stack">
            <TimelineChart
              stats={stats}
              selectedStatsIndex={selectedStatsIndex}
              onSelectRun={selectRun}
            />

            <div className="runs-table-wrap">
              <table className="runs-table">
                <thead>
                  <tr>
                    <th>Run</th>
                    <th>Duration</th>
                    <th>Healthy feeds</th>
                    <th>Sent</th>
                  </tr>
                </thead>
                <tbody>
                  {stats.slice(0, 12).map((run, index) => {
                    const runHealthy = healthyFeedCount(run);
                    const isSelected = runContextMode === "selected" &&
                      index === selectedStatsIndex;
                    return (
                      <tr
                        key={run.start_time ?? `${index}`}
                        className={isSelected ? "selected" : ""}
                        onClick={() => {
                          selectRun(index);
                        }}
                      >
                        <td>{formatDateTime(run.start_time)}</td>
                        <td>{formatDuration(run.duration)}</td>
                        <td>{runHealthy}/{toNumber(run.total_feeds)}</td>
                        <td>{toNumber(run.messages_sent)}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>

            <details className="detail-toggle">
              <summary>Show detailed analytics for this run</summary>
              <ChartsGrid>
                <OutcomeCharts stats={stats} activeRun={activeRun} />
                <NetworkCharts activeRun={activeRun} />
                <PerformanceCharts activeRun={activeRun} />
              </ChartsGrid>
            </details>
          </div>
        )}
      </section>

      <TopFeedsPanels activeRun={activeRun} />
    </div>
  );
}

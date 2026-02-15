import React, { useMemo } from "npm:react";

import { formatBytes, formatDateTime, formatDuration } from "../format.ts";
import { StatsRun } from "../types.ts";
import { RunChart } from "./RunChart.tsx";

/**
 * Formats an ISO timestamp into a short relative string.
 */
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

/**
 * Renders dashboard stats indicators, charts, and run drill-down sections.
 */
export function StatsView(props: {
  stats: StatsRun[];
  statsLoading: boolean;
  statsError: string;
  selectedStatsIndex: number;
  setSelectedStatsIndex: (index: number) => void;
  loadStats: () => Promise<void>;
}) {
  const { stats, statsLoading, statsError, selectedStatsIndex, setSelectedStatsIndex, loadStats } = props;
  const latestStats = stats.length > 0 ? stats[0] : undefined;
  const selectedStats = stats[selectedStatsIndex] ?? latestStats;

  const successRate = useMemo(() => {
    if (!latestStats || !latestStats.total_feeds || latestStats.total_feeds === 0) {
      return "n/a";
    }
    const ratio = ((latestStats.success_feeds ?? 0) / latestStats.total_feeds) * 100;
    return `${ratio.toFixed(1)}%`;
  }, [latestStats]);

  const failureRate = useMemo(() => {
    if (!latestStats || !latestStats.total_feeds || latestStats.total_feeds === 0) {
      return "n/a";
    }
    const ratio = ((latestStats.failed_feeds ?? 0) / latestStats.total_feeds) * 100;
    return `${ratio.toFixed(1)}%`;
  }, [latestStats]);

  return (
    <div className="column">
      <RunChart
        stats={stats}
        selectedStatsIndex={selectedStatsIndex}
        setSelectedStatsIndex={setSelectedStatsIndex}
      />

      <section className="panel stats-panel">
        <header className="panel-header">
          <div>
            <h2>Run Overview</h2>
            <p>Aggregated metrics from persisted run snapshots.</p>
          </div>
          <button className="button button-ghost" type="button" onClick={() => void loadStats()} disabled={statsLoading}>
            {statsLoading ? "Loading..." : "Refresh stats"}
          </button>
        </header>

        {statsError && <p className="message message-error">{statsError}</p>}
        {!statsError && stats.length === 0 && !statsLoading && <p className="message message-info">No stats available yet.</p>}

        {latestStats && (
          <>
            <div className="indicator-grid">
              <article className="indicator indicator-primary">
                <p>Last run</p>
                <h3>{formatRelativeTime(latestStats.start_time)}</h3>
                <span>{formatDateTime(latestStats.start_time)}</span>
              </article>
              <article className="indicator">
                <p>Total feeds</p>
                <h3>{latestStats.total_feeds ?? 0}</h3>
                <span>Success rate: {successRate}</span>
              </article>
              <article className="indicator indicator-danger">
                <p>Failed feeds</p>
                <h3>{latestStats.failed_feeds ?? 0}</h3>
                <span>Failure rate: {failureRate}</span>
              </article>
              <article className="indicator">
                <p>Run duration</p>
                <h3>{formatDuration(latestStats.duration)}</h3>
                <span>Memory: {formatBytes(latestStats.memory_usage)}</span>
              </article>
            </div>

            <div className="metric-grid">
              <article className="metric">
                <p>Messages attempted</p>
                <h3>{latestStats.messages_attempted ?? 0}</h3>
              </article>
              <article className="metric">
                <p>Messages sent</p>
                <h3>{latestStats.messages_sent ?? 0}</h3>
              </article>
              <article className="metric">
                <p>Messages failed</p>
                <h3>{latestStats.messages_failed ?? 0}</h3>
              </article>
              <article className="metric">
                <p>Items seen</p>
                <h3>{latestStats.items_seen_total ?? 0}</h3>
              </article>
              <article className="metric">
                <p>Items deduped</p>
                <h3>{latestStats.items_deduped_total ?? 0}</h3>
              </article>
              <article className="metric">
                <p>Retries</p>
                <h3>{latestStats.fetch_retries_total ?? 0}</h3>
              </article>
            </div>

            <div className="runs-table-wrap">
              <table className="runs-table">
                <thead>
                  <tr>
                    <th>Run</th>
                    <th>Duration</th>
                    <th>Feeds</th>
                    <th>Sent</th>
                  </tr>
                </thead>
                <tbody>
                  {stats.slice(0, 10).map((run, index) => (
                    <tr
                      key={run.start_time ?? `${index}`}
                      className={index === selectedStatsIndex ? "selected" : ""}
                      onClick={() => {
                        setSelectedStatsIndex(index);
                      }}
                    >
                      <td>{formatDateTime(run.start_time)}</td>
                      <td>{formatDuration(run.duration)}</td>
                      <td>
                        {run.success_feeds ?? 0}/{run.total_feeds ?? 0}
                      </td>
                      <td>{run.messages_sent ?? 0}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
      </section>

      {selectedStats && (
        <section className="panel feed-panel">
          <header className="panel-header">
            <div>
              <h2>Top Slow Feeds</h2>
              <p>Feeds with the highest fetch duration for this run.</p>
            </div>
          </header>
          <ol className="feed-list">
            {(selectedStats.top_slowest_feeds ?? []).map((item, index) => (
              <li key={`${item.url ?? "unknown"}-${index}`}>
                <div>
                  <strong>{item.url ?? "unknown feed"}</strong>
                  <span>Status class: {item.last_status_class ?? 0}</span>
                </div>
                <b>{formatDuration(item.fetch_duration)}</b>
              </li>
            ))}
            {(selectedStats.top_slowest_feeds ?? []).length === 0 && <li className="empty-line">No entries</li>}
          </ol>
        </section>
      )}
    </div>
  );
}

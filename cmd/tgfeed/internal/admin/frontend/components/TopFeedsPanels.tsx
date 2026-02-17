import React from "react";

import { formatDuration } from "../format.ts";
import { FeedSummary, StatsRun } from "../types.ts";

function feedName(item: FeedSummary): string {
  return item.url ?? "unknown feed";
}

function feedMetric(value: number | undefined): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return 0;
  }
  return value;
}

/** Renders top-feed summaries for the selected run context. */
export function TopFeedsPanels(props: { activeRun: StatsRun | undefined }) {
  const { activeRun } = props;
  const slowest = activeRun?.top_slowest_feeds ?? [];
  const errors = activeRun?.top_error_feeds ?? [];
  const newItems = activeRun?.top_new_item_feeds ?? [];

  return (
    <section className="panel feed-panel">
      <header className="panel-header">
        <div>
          <h2>Feed Insights</h2>
          <p>Top feed contributors for the active run context.</p>
        </div>
      </header>

      <div className="feed-groups">
        <article className="feed-group">
          <h3>Slowest feeds</h3>
          <ol className="feed-list">
            {slowest.map((item, index) => (
              <li key={`${item.url ?? "unknown"}-${index}`}>
                <div>
                  <strong>{feedName(item)}</strong>
                  <span>
                    Status class: {feedMetric(item.last_status_class)}
                  </span>
                </div>
                <b>{formatDuration(item.fetch_duration)}</b>
              </li>
            ))}
            {slowest.length === 0 && <li className="empty-line">No entries</li>}
          </ol>
        </article>

        <article className="feed-group">
          <h3>Most error-prone</h3>
          <ol className="feed-list">
            {errors.map((item, index) => (
              <li key={`${item.url ?? "unknown"}-${index}`}>
                <div>
                  <strong>{feedName(item)}</strong>
                  <span>
                    Failures: {feedMetric(item.failures)} · Retries:{" "}
                    {feedMetric(item.retries)}
                  </span>
                </div>
                <b>{feedMetric(item.failures)}</b>
              </li>
            ))}
            {errors.length === 0 && <li className="empty-line">No entries</li>}
          </ol>
        </article>

        <article className="feed-group">
          <h3>Most new items</h3>
          <ol className="feed-list">
            {newItems.map((item, index) => (
              <li key={`${item.url ?? "unknown"}-${index}`}>
                <div>
                  <strong>{feedName(item)}</strong>
                  <span>Retries: {feedMetric(item.retries)}</span>
                </div>
                <b>{feedMetric(item.items_enqueued)} items</b>
              </li>
            ))}
            {newItems.length === 0 && (
              <li className="empty-line">
                No entries
              </li>
            )}
          </ol>
        </article>
      </div>
    </section>
  );
}

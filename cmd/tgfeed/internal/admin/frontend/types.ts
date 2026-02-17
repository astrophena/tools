import React from "react";

/** Lightweight summary for top feed lists in stats payloads. */
export type FeedSummary = {
  url?: string;
  fetch_duration?: number;
  failures?: number;
  items_enqueued?: number;
  retries?: number;
  last_status_class?: number;
};

/**
 * Stats payload shape returned by `/api/stats`.
 */
export type StatsRun = {
  start_time?: string;
  duration?: number;
  total_feeds?: number;
  success_feeds?: number;
  failed_feeds?: number;
  not_modified_feeds?: number;

  total_fetch_time?: number;
  avg_fetch_time?: number;
  fetch_latency_ms?: { p50?: number; p90?: number; p99?: number; max?: number };
  send_latency_ms?: { p50?: number; p90?: number; p99?: number; max?: number };

  http_2xx_count?: number;
  http_3xx_count?: number;
  http_4xx_count?: number;
  http_5xx_count?: number;
  timeout_count?: number;
  network_error_count?: number;
  parse_error_count?: number;

  items_seen_total?: number;
  items_kept_total?: number;
  items_deduped_total?: number;
  items_skipped_old_total?: number;
  items_enqueued_total?: number;

  messages_attempted?: number;
  messages_sent?: number;
  messages_failed?: number;

  fetch_retries_total?: number;
  feeds_retried_count?: number;
  backoff_sleep_total?: number;
  special_rate_limit_retries?: number;

  memory_usage?: number;

  top_slowest_feeds?: FeedSummary[];
  top_error_feeds?: FeedSummary[];
  top_new_item_feeds?: FeedSummary[];
  [key: string]: unknown;
};

/**
 * Shared mutable editor state consumed by config/template panels.
 */
export type EditableResource = {
  value: string;
  setValue: React.Dispatch<React.SetStateAction<string>>;
  dirty: boolean;
  loading: boolean;
  saving: boolean;
  error: string;
  load: () => Promise<void>;
  save: () => Promise<boolean>;
};

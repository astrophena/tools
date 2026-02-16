/**
 * Formats an ISO datetime string using a fixed day.month.year and 24-hour clock.
 */
export function formatDateTime(value: string | undefined): string {
  if (!value) {
    return "n/a";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  const day = date.getDate().toString().padStart(2, "0");
  const month = (date.getMonth() + 1).toString().padStart(2, "0");
  const year = date.getFullYear();
  const hours = date.getHours().toString().padStart(2, "0");
  const minutes = date.getMinutes().toString().padStart(2, "0");
  const seconds = date.getSeconds().toString().padStart(2, "0");
  return `${day}.${month}.${year}, ${hours}:${minutes}:${seconds}`;
}

/**
 * Formats nanosecond duration values into compact human-readable text.
 */
export function formatDuration(ns: number | undefined): string {
  if (typeof ns !== "number" || !Number.isFinite(ns) || ns < 0) {
    return "n/a";
  }
  const ms = ns / 1_000_000;
  if (ms < 1_000) {
    return `${ms.toFixed(0)} ms`;
  }
  const sec = ms / 1_000;
  if (sec < 60) {
    return `${sec.toFixed(1)} s`;
  }
  const min = Math.floor(sec / 60);
  const rem = sec % 60;
  return `${min}m ${rem.toFixed(0)}s`;
}

/**
 * Formats byte values using binary units from B to TB.
 */
export function formatBytes(bytes: number | undefined): string {
  if (typeof bytes !== "number" || !Number.isFinite(bytes) || bytes < 0) {
    return "n/a";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const precision = unit === 0 ? 0 : 1;
  return `${value.toFixed(precision)} ${units[unit]}`;
}

CREATE TABLE IF NOT EXISTS app_telemetry_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  app_name TEXT NOT NULL,
  app_version TEXT NOT NULL,
  os TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

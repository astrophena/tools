CREATE TABLE IF NOT EXISTS app_telemetry_events (
  id SERIAL PRIMARY KEY,
  session_id VARCHAR NOT NULL,
  app_name VARCHAR NOT NULL,
  app_version VARCHAR NOT NULL,
  os VARCHAR NOT NULL,
  event_type VARCHAR NOT NULL,
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

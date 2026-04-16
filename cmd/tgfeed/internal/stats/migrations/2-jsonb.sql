CREATE TABLE IF NOT EXISTS runs_new (
  run_id INTEGER PRIMARY KEY,
  started_at_unix INTEGER NOT NULL,
  finished_at_unix INTEGER NOT NULL,
  duration_ms INTEGER NOT NULL,
  payload_json BLOB NOT NULL CHECK (json_valid(payload_json, 5)),
  payload_sha256 TEXT NOT NULL,
  total_feeds INTEGER GENERATED ALWAYS AS (json_extract(payload_json, '$.total_feeds')) STORED,
  success_feeds INTEGER GENERATED ALWAYS AS (json_extract(payload_json, '$.success_feeds')) STORED,
  failed_feeds INTEGER GENERATED ALWAYS AS (json_extract(payload_json, '$.failed_feeds')) STORED,
  messages_failed INTEGER GENERATED ALWAYS AS (json_extract(payload_json, '$.messages_failed')) STORED
) STRICT;

INSERT INTO runs_new(run_id, started_at_unix, finished_at_unix, duration_ms, payload_json, payload_sha256)
SELECT run_id, started_at_unix, finished_at_unix, duration_ms, jsonb(payload_json), payload_sha256
FROM runs;

DROP TABLE runs;
ALTER TABLE runs_new RENAME TO runs;

CREATE UNIQUE INDEX IF NOT EXISTS runs_started_payload_uq ON runs(started_at_unix, payload_sha256);
CREATE INDEX IF NOT EXISTS runs_started_desc_idx ON runs(started_at_unix DESC);
CREATE INDEX IF NOT EXISTS runs_failed_started_idx ON runs(failed_feeds DESC, started_at_unix DESC);
CREATE INDEX IF NOT EXISTS runs_problematic_partial_idx ON runs(started_at_unix DESC)
WHERE failed_feeds > 0 OR messages_failed > 0;

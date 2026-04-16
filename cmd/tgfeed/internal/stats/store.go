// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package stats

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/tailscale/sqlite"
)

const dbFileName = "stats.sqlite3"
const defaultListLimit = 100

const currentSchemaVersion = 2

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store persists and queries run snapshots from a SQLite database.
type Store struct {
	db       *sql.DB
	dbMu     sync.Mutex
	path     string
	uri      string
	readOnly bool
}

// OpenWriter opens a read-write store for tgfeed run mode.
func OpenWriter(stateDir string) *Store {
	path := filepath.Join(stateDir, dbFileName)
	return &Store{
		path: path,
		uri:  sqliteFileURI(path),
	}
}

// OpenReader opens a read-only store for tgfeed admin mode.
func OpenReader(stateDir string) *Store {
	path := filepath.Join(stateDir, dbFileName)
	return &Store{
		path:     path,
		uri:      sqliteFileURI(path) + "?mode=ro",
		readOnly: true,
	}
}

// OpenMemory opens a shared in-memory store suitable for tests.
func OpenMemory(name string) *Store {
	name = strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(name)
	if name == "" {
		name = "tgfeed_stats"
	}
	return &Store{
		path: "file:/" + name + "?vfs=memdb",
		uri:  "file:/" + name + "?vfs=memdb",
	}
}

// Path returns the absolute SQLite path.
func (s *Store) Path() string { return s.path }

// Close releases the underlying database handle.
func (s *Store) Close() error {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()

	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// Bootstrap creates schema and configures SQLite pragmas.
func (s *Store) Bootstrap(ctx context.Context) error {
	if s.readOnly {
		return nil
	}
	if !strings.Contains(s.uri, "vfs=memdb") {
		if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
			return err
		}
	}
	db, err := s.open(ctx)
	if err != nil {
		return err
	}

	return s.bootstrapMigrations(ctx, db)
}

// SaveRun appends a run snapshot.
func (s *Store) SaveRun(ctx context.Context, run *Run) error {
	payload, err := json.Marshal(run)
	if err != nil {
		return err
	}
	started := run.StartTime.UTC().Unix()
	finished := run.StartTime.Add(run.Duration).UTC().Unix()
	durationMS := run.Duration.Milliseconds()
	hash := sha256.Sum256(payload)
	hashHex := hex.EncodeToString(hash[:])

	db, err := s.open(ctx)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO runs(started_at_unix, finished_at_unix, duration_ms, payload_json, payload_sha256)
		VALUES(?, ?, ?, jsonb(?), ?);`,
		started,
		finished,
		durationMS,
		string(payload),
		hashHex,
	)
	return err
}

// ListRuns returns latest run JSON payloads.
func (s *Store) ListRuns(ctx context.Context, limit int) ([]json.RawMessage, error) {
	return s.listRuns(ctx, limit, nil)
}

// ListRunsBefore returns latest run JSON payloads older than beforeStartedAt.
func (s *Store) ListRunsBefore(ctx context.Context, limit int, beforeStartedAt int64) ([]json.RawMessage, error) {
	return s.listRuns(ctx, limit, &beforeStartedAt)
}

func (s *Store) listRuns(ctx context.Context, limit int, beforeStartedAt *int64) ([]json.RawMessage, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}

	limit = normalizeLimit(limit)
	query := `SELECT json(payload_json) FROM runs`
	args := []any{}
	if beforeStartedAt != nil {
		query += ` WHERE started_at_unix < ?`
		args = append(args, *beforeStartedAt)
	}
	query += ` ORDER BY started_at_unix DESC LIMIT ?;`
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []json.RawMessage
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		res = append(res, json.RawMessage(payload))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return res, nil
}

// ListRunSummaries returns lightweight run rows for dashboard overview panels.
func (s *Store) ListRunSummaries(ctx context.Context, limit int, beforeStartedAt *int64) ([]RunSummary, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}

	limit = normalizeLimit(limit)
	query := `SELECT
	started_at_unix,
	duration_ms,
	total_feeds,
	success_feeds,
	failed_feeds,
	COALESCE(CAST(json_extract(payload_json, '$.not_modified_feeds') AS INTEGER), 0) AS not_modified_feeds,
	COALESCE(CAST(json_extract(payload_json, '$.messages_attempted') AS INTEGER), 0) AS messages_attempted,
	COALESCE(CAST(json_extract(payload_json, '$.messages_sent') AS INTEGER), 0) AS messages_sent,
	COALESCE(CAST(json_extract(payload_json, '$.messages_failed') AS INTEGER), 0) AS messages_failed,
	COALESCE(CAST(json_extract(payload_json, '$.fetch_latency_ms.p50') AS INTEGER), 0) AS fetch_latency_p50,
	COALESCE(CAST(json_extract(payload_json, '$.fetch_latency_ms.p90') AS INTEGER), 0) AS fetch_latency_p90,
	COALESCE(CAST(json_extract(payload_json, '$.fetch_latency_ms.p99') AS INTEGER), 0) AS fetch_latency_p99,
	COALESCE(CAST(json_extract(payload_json, '$.fetch_latency_ms.max') AS INTEGER), 0) AS fetch_latency_max,
	COALESCE(CAST(json_extract(payload_json, '$.memory_usage') AS INTEGER), 0) AS memory_usage
FROM runs`
	args := []any{}
	if beforeStartedAt != nil {
		query += ` WHERE started_at_unix < ?`
		args = append(args, *beforeStartedAt)
	}
	query += ` ORDER BY started_at_unix DESC LIMIT ?;`
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []RunSummary
	for rows.Next() {
		var (
			item       RunSummary
			durationMS int64
			memory     int64
		)
		if err := rows.Scan(
			&item.StartedAtUnix,
			&durationMS,
			&item.TotalFeeds,
			&item.SuccessFeeds,
			&item.FailedFeeds,
			&item.NotModifiedFeeds,
			&item.MessagesAttempted,
			&item.MessagesSent,
			&item.MessagesFailed,
			&item.FetchLatencyMS.P50,
			&item.FetchLatencyMS.P90,
			&item.FetchLatencyMS.P99,
			&item.FetchLatencyMS.Max,
			&memory,
		); err != nil {
			return nil, err
		}
		item.StartTime = time.Unix(item.StartedAtUnix, 0).UTC()
		item.Duration = time.Duration(durationMS) * time.Millisecond
		item.MemoryUsage = uint64(max(memory, 0))
		res = append(res, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return res, nil
}

// GetRunByStartedAt returns a full JSON payload for a run with the given start time.
func (s *Store) GetRunByStartedAt(ctx context.Context, startedAtUnix int64) (json.RawMessage, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}

	var payload string
	err = db.QueryRowContext(
		ctx,
		`SELECT json(payload_json) FROM runs WHERE started_at_unix = ? LIMIT 1;`,
		startedAtUnix,
	).Scan(&payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(payload), nil
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultListLimit
	}
	return limit
}

func (s *Store) open(ctx context.Context) (*sql.DB, error) {
	s.dbMu.Lock()
	defer s.dbMu.Unlock()

	if s.db != nil {
		return s.db, nil
	}

	db, err := sql.Open("sqlite3", sqliteURIWithPragma(s.uri, "busy_timeout(5000)"))
	if err != nil {
		return nil, fmt.Errorf("open SQLite %q: %w", s.path, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping SQLite %q: %w", s.path, err)
	}
	s.db = db
	return db, nil
}

func (s *Store) bootstrapMigrations(ctx context.Context, db *sql.DB) error {
	version, err := schemaVersion(ctx, db)
	if err != nil {
		return err
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("stats schema version %d is newer than supported version %d", version, currentSchemaVersion)
	}

	migrations, err := migrationFiles()
	if err != nil {
		return err
	}
	if len(migrations) != currentSchemaVersion {
		return fmt.Errorf("stats migrations count %d does not match current schema version %d", len(migrations), currentSchemaVersion)
	}
	for _, migration := range migrations {
		if migration.version <= version {
			continue
		}
		sqlBytes, err := fs.ReadFile(migrationsFS, migration.path)
		if err != nil {
			return fmt.Errorf("read stats migration %q: %w", migration.path, err)
		}
		if err := execScript(ctx, db, string(sqlBytes)); err != nil {
			return fmt.Errorf("apply stats migration %q: %w", migration.path, err)
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d;", migration.version)); err != nil {
			return fmt.Errorf("set stats schema version %d: %w", migration.version, err)
		}
	}
	return nil
}

type migrationFile struct {
	version int
	path    string
}

func migrationFiles() ([]migrationFile, error) {
	entries, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	res := make([]migrationFile, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSuffix(filepath.Base(entry), ".sql")
		versionText, _, ok := strings.Cut(name, "-")
		if !ok {
			return nil, fmt.Errorf("invalid stats migration filename %q", entry)
		}
		version, err := strconv.Atoi(versionText)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid stats migration version %q", entry)
		}
		res = append(res, migrationFile{
			version: version,
			path:    entry,
		})
	}
	slices.SortFunc(res, func(a migrationFile, b migrationFile) int {
		return a.version - b.version
	})
	for i, migration := range res {
		wantVersion := i + 1
		if migration.version != wantVersion {
			return nil, fmt.Errorf("missing stats migration %d", wantVersion)
		}
	}
	return res, nil
}

func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version;`).Scan(&version); err != nil {
		return 0, fmt.Errorf("query stats schema version: %w", err)
	}
	return version, nil
}

func sqliteFileURI(path string) string {
	return "file:" + filepath.ToSlash(path)
}

func sqliteURIWithPragma(uri string, pragma string) string {
	sep := "?"
	if strings.Contains(uri, "?") {
		sep = "&"
	}
	return uri + sep + "_pragma=" + pragma
}

func execScript(ctx context.Context, db *sql.DB, script string) error {
	for _, stmt := range strings.Split(script, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

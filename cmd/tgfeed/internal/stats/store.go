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

	_ "github.com/tailscale/sqlite"
)

const dbFileName = "stats.sqlite3"

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
	if limit <= 0 {
		limit = 100
	}
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(
		ctx,
		`SELECT json(payload_json) FROM runs ORDER BY started_at_unix DESC LIMIT ?;`,
		limit,
	)
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

// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package store

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/tailscale/sqlite"
)

// SQLiteStore is a SQLite implementation of the [Store] interface.
type SQLiteStore struct {
	db  *sql.DB
	ttl time.Duration
}

// NewSQLiteStore creates a new [SQLiteStore] and connects to the database.
func NewSQLiteStore(ctx context.Context, dsn string, ttl time.Duration) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL;"); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON;"); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA strict=ON;"); err != nil {
		return nil, err
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS kv (
			key TEXT PRIMARY KEY,
			value BLOB NOT NULL,
			last_accessed INTEGER NOT NULL
		);
	`); err != nil {
		return nil, err
	}

	s := &SQLiteStore{
		db:  db,
		ttl: ttl,
	}
	s.cleanup(ctx, true)
	go s.cleanup(ctx, false)

	return s, nil
}

func (s *SQLiteStore) cleanup(ctx context.Context, firstRun bool) {
	if firstRun {
		s.performCleanup(ctx)
		return
	}

	sleepDuration := s.ttl / 2
	if sleepDuration > 24*time.Hour {
		sleepDuration = 24 * time.Hour
	}

	for {
		select {
		case <-time.After(sleepDuration):
			s.performCleanup(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (s *SQLiteStore) performCleanup(ctx context.Context) {
	s.db.ExecContext(ctx, `DELETE FROM kv WHERE last_accessed < ?;`, time.Now().Add(-s.ttl).Unix())
}

// Get retrieves a value for a given key.
func (s *SQLiteStore) Get(ctx context.Context, key string) ([]byte, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var data []byte
	if err := tx.QueryRowContext(ctx, `
		SELECT value FROM kv WHERE key = ? AND last_accessed >= ?;
	`, key, time.Now().Add(-s.ttl).Unix()).Scan(&data); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE kv SET last_accessed = ? WHERE key = ?;
	`, time.Now().Unix(), key); err != nil {
		return nil, err
	}

	return data, tx.Commit()
}

// Set stores a value for a given key.
func (s *SQLiteStore) Set(ctx context.Context, key string, value []byte) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO kv (key, value, last_accessed)
		VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE
		SET value = excluded.value, last_accessed = excluded.last_accessed;
	`, key, value, time.Now().Unix())
	return err
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

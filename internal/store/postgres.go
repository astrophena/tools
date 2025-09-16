// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is a PostgreSQL implementation of the Store interface.
type PostgresStore struct {
	pool *pgxpool.Pool
	ttl  time.Duration
}

// NewPostgresStore creates a new PostgresStore and connects to the database.
func NewPostgresStore(ctx context.Context, databaseURL string, ttl time.Duration) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}

	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS kv (
			key TEXT PRIMARY KEY,
			value JSONB NOT NULL,
			last_accessed TIMESTAMPTZ NOT NULL
		);
	`); err != nil {
		return nil, err
	}

	s := &PostgresStore{
		pool: pool,
		ttl:  ttl,
	}
	s.cleanup(ctx, true)
	go s.cleanup(ctx, false)
	return s, nil
}

func (s *PostgresStore) cleanup(ctx context.Context, firstRun bool) {
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

func (s *PostgresStore) performCleanup(ctx context.Context) {
	s.pool.Exec(ctx, `DELETE FROM kv WHERE last_accessed < NOW() - $1;`, s.ttl.String())
}

// Get retrieves a value for a given key.
func (s *PostgresStore) Get(ctx context.Context, key string) ([]byte, error) {
	var data []byte
	if err := s.pool.QueryRow(ctx, `
		UPDATE kv SET last_accessed = NOW() WHERE key = $1
		RETURNING value;
	`, key).Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// Set stores a value for a given key.
func (s *PostgresStore) Set(ctx context.Context, key string, value []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO kv (key, value, last_accessed)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE
		SET value = $2, last_accessed = NOW();
	`, key, value)
	return err
}

// Close closes the database connection.
func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}

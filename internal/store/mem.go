// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package store

import (
	"context"
	"time"

	"go.astrophena.name/base/syncx"
)

// MemStore is an in-memory implementation of the [Store] interface.
type MemStore struct {
	ttl   time.Duration
	cache syncx.Map[string, cacheEntry]
}

// NewMemStore creates a new [MemStore] with the given TTL.
func NewMemStore(ctx context.Context, ttl time.Duration) *MemStore {
	s := &MemStore{
		ttl: ttl,
	}
	s.cleanup(ctx, true)
	go s.cleanup(ctx, false)
	return s
}

type cacheEntry struct {
	value        []byte
	lastAccessed time.Time
}

func (s *MemStore) cleanup(ctx context.Context, firstRun bool) {
	if firstRun {
		s.performCleanup()
		return
	}

	sleepDuration := min(s.ttl/2, 24*time.Hour)

	for {
		select {
		case <-time.After(sleepDuration):
			s.performCleanup()
		case <-ctx.Done():
			return
		}
	}
}

func (s *MemStore) performCleanup() {
	s.cache.Range(func(key string, entry cacheEntry) bool {
		if time.Since(entry.lastAccessed) > s.ttl {
			s.cache.Delete(key)
		}
		return true
	})
}

// Get retrieves a value for a given key.
func (s *MemStore) Get(_ context.Context, key string) ([]byte, error) {
	entry, ok := s.cache.Load(key)
	if !ok {
		return nil, nil
	}

	if time.Since(entry.lastAccessed) > s.ttl {
		s.cache.Delete(key)
		return nil, nil
	}

	entry.lastAccessed = time.Now()
	s.cache.Store(key, entry)

	// Return a copy to prevent the caller from mutating the cache.
	return append([]byte(nil), entry.value...), nil
}

// Set stores a value for a given key.
func (s *MemStore) Set(_ context.Context, key string, value []byte) error {
	// Store a copy to prevent the caller from mutating the cache.
	valueCopy := append([]byte(nil), value...)
	s.cache.Store(key, cacheEntry{
		value:        valueCopy,
		lastAccessed: time.Now(),
	})
	return nil
}

// Close is a no-op for MemStore.
func (s *MemStore) Close() error {
	return nil
}

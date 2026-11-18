// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package cache

import (
	"context"
	"sync"
	"time"
)

// MemStore stores values in memory.
type MemStore struct {
	ttl time.Duration

	mu    sync.Mutex
	cache map[string]entry
}

// NewMemStore creates a cache with the given TTL. The context controls the
// lifetime of its background eviction loop.
func NewMemStore(ctx context.Context, ttl time.Duration) *MemStore {
	s := &MemStore{
		ttl: ttl,
	}
	go evictPeriodically(ctx, ttl, s.evictExpired)
	return s
}

func (s *MemStore) evictExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, e := range s.cache {
		if time.Since(e.LastAccessed) > s.ttl {
			delete(s.cache, key)
		}
	}
}

// Get retrieves a copy of the stored value and refreshes its expiration.
func (s *MemStore) Get(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.cache[key]
	if !ok {
		return nil, nil
	}

	if time.Since(e.LastAccessed) > s.ttl {
		delete(s.cache, key)
		return nil, nil
	}

	e.LastAccessed = time.Now()
	s.cache[key] = e
	return append([]byte(nil), e.Value...), nil
}

// Set stores a copy of value and refreshes its expiration.
func (s *MemStore) Set(key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cache == nil {
		s.cache = make(map[string]entry)
	}
	s.cache[key] = entry{
		Value:        append([]byte(nil), value...),
		LastAccessed: time.Now(),
	}
	return nil
}

// Close returns nil. Eviction stops when the constructor's context is canceled.
func (s *MemStore) Close() error { return nil }

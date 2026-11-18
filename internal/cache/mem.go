// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package cache

import (
	"context"
	"sync"
	"time"
)

// Mem stores values in memory.
type Mem struct {
	ttl time.Duration

	mu    sync.Mutex
	cache map[string]entry
}

// NewMem creates a cache with the given TTL. The context controls the
// lifetime of its background eviction loop.
func NewMem(ctx context.Context, ttl time.Duration) *Mem {
	s := &Mem{
		ttl: ttl,
	}
	go evictPeriodically(ctx, ttl, s.evictExpired)
	return s
}

func (m *Mem) evictExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, e := range m.cache {
		if time.Since(e.LastAccessed) > m.ttl {
			delete(m.cache, key)
		}
	}
}

// Get retrieves a copy of the stored value and refreshes its expiration.
func (m *Mem) Get(key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.cache[key]
	if !ok {
		return nil, nil
	}

	if time.Since(e.LastAccessed) > m.ttl {
		delete(m.cache, key)
		return nil, nil
	}

	e.LastAccessed = time.Now()
	m.cache[key] = e
	return append([]byte(nil), e.Value...), nil
}

// Set stores a copy of value and refreshes its expiration.
func (m *Mem) Set(key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cache == nil {
		m.cache = make(map[string]entry)
	}
	m.cache[key] = entry{
		Value:        append([]byte(nil), value...),
		LastAccessed: time.Now(),
	}
	return nil
}

// Close returns nil. Eviction stops when the constructor's context is canceled.
func (m *Mem) Close() error { return nil }

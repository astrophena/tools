// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package cache provides in-memory and file-backed caches.
package cache

import (
	"context"
	"time"
)

// Store is a byte-valued cache with expiration based on last access.
type Store interface {
	// Get retrieves a value and refreshes its expiration. A missing or expired
	// key returns (nil, nil).
	Get(key string) ([]byte, error)
	// Set stores a value and refreshes its expiration.
	Set(key string, value []byte) error
	// Close finishes any pending persistence work.
	Close() error
}

type entry struct {
	Value        []byte    `json:"value"`
	LastAccessed time.Time `json:"last_accessed"`
}

func evictPeriodically(ctx context.Context, ttl time.Duration, evict func()) {
	// Keep short or nonpositive TTLs from spinning the cleanup loop.
	interval := min(max(ttl/2, time.Millisecond), 24*time.Hour)
	for ctx.Err() == nil {
		select {
		case <-time.After(interval):
			evict()
		case <-ctx.Done():
			return
		}
	}
}

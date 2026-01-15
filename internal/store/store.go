// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package store implements a key-value store backed in-memory or by JSON file.
package store

import (
	"context"
)

// Store is a generic interface for a key-value store.
type Store interface {
	// Get retrieves a value for a given key.
	// It must return (nil, nil) if the key is not found.
	Get(ctx context.Context, key string) ([]byte, error)
	// Set stores a value for a given key.
	Set(ctx context.Context, key string, value []byte) error
	// Close closes the store and releases any resources.
	Close() error
}

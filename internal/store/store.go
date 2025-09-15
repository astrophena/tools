// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package store implements a key-value store backed in-memory or by PostgreSQL.
package store

import (
	"context"

	"go.starlark.net/starlark"
)

// Store is a generic interface for a key-value store.
type Store interface {
	// Get retrieves a value for a given key.
	Get(ctx context.Context, key string) (starlark.Value, error)
	// Set stores a value for a given key.
	Set(ctx context.Context, key string, value starlark.Value) error
	// Close closes the store and releases any resources.
	Close() error
}

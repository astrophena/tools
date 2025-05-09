// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package kvcache implements a Starlark module for a simple key-value cache
// with time-to-live (TTL) expiration based on last access time.
package kvcache

import (
	_ "embed"
	"time"

	"go.astrophena.name/base/syncx"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Module returns a Starlark module that exposes key-value caching functionality.
//
// This module provides two functions:
//
//   - get(key: str) -> value | None: Retrieves the value associated with the
//     given string key. Returns the stored value if the key exists and has
//     not expired. Returns None if the key is not found or if the entry
//     has expired. Accessing a key resets its TTL timer.
//   - set(key: str, value: any): Stores the given value under the specified
//     string key. Any existing value for the key is overwritten. Storing a
//     value resets the TTL timer for that key.
//
// The ttl argument specifies the time-to-live duration. A cache entry will
// expire if it hasn't been accessed (via get) or updated (via set) for
// longer than this duration.
func Module(ttl time.Duration) *starlarkstruct.Module {
	m := &module{
		ttl: ttl,
	}
	return &starlarkstruct.Module{
		Name: "kvcache",
		Members: starlark.StringDict{
			"get": starlark.NewBuiltin("kvcache.get", m.get),
			"set": starlark.NewBuiltin("kvcache.set", m.set),
		},
	}
}

type module struct {
	ttl   time.Duration
	cache syncx.Map[string, *cacheEntry]
}

// cacheEntry stores the cached value and its last access time.
type cacheEntry struct {
	value        starlark.Value
	lastAccessed time.Time
}

func (m *module) get(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key", &key); err != nil {
		return nil, err
	}

	entry, ok := m.cache.Load(key)
	if !ok {
		return starlark.None, nil
	}

	// Check if the entry has expired based on last access time.
	if time.Since(entry.lastAccessed) > m.ttl {
		m.cache.Delete(key)
		return starlark.None, nil
	}

	entry.lastAccessed = time.Now()
	m.cache.Store(key, entry)

	return entry.value, nil
}

func (m *module) set(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		key   string
		value starlark.Value
	)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key", &key, "value", &value); err != nil {
		return nil, err
	}
	entry := &cacheEntry{
		value:        value,
		lastAccessed: time.Now(),
	}
	m.cache.Store(key, entry)

	return starlark.None, nil
}

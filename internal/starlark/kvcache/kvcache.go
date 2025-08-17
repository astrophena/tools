// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package kvcache

import (
	"context"
	_ "embed"
	"time"

	"go.astrophena.name/base/syncx"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Module returns a Starlark module that exposes key-value caching functionality.
//
// The ttl argument specifies the time-to-live duration. A cache entry will
// expire if it hasn't been accessed (via get) or updated (via set) for
// longer than this duration.
func Module(ctx context.Context, ttl time.Duration) *starlarkstruct.Module {
	m := &module{
		ttl: ttl,
	}
	go m.cleanup(ctx)
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
	cache syncx.Map[string, cacheEntry]
}

func (m *module) cleanup(ctx context.Context) {
	ticker := time.NewTicker(m.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.cache.Range(func(key string, entry cacheEntry) bool {
				if time.Since(entry.lastAccessed) > m.ttl {
					m.cache.Delete(key)
				}
				return true
			})
		case <-ctx.Done():
			return
		}
	}
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
	m.cache.Store(key, cacheEntry{
		value:        value,
		lastAccessed: time.Now(),
	})
	return starlark.None, nil
}

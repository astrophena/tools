// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package syncmap provides a generic version of [sync.Map].
package syncmap

import (
	"sync"
)

// Map is a generic version of [sync.Map].
type Map[K, V comparable] struct{ m *sync.Map }

// NewMap returns a new [Map].
func NewMap[K, V comparable]() *Map[K, V] { return &Map[K, V]{m: &sync.Map{}} }

// Load is [sync.Map.Load].
func (m *Map[K, V]) Load(key K) (value V, ok bool) {
	val, ok := m.m.Load(key)
	if !ok {
		return value, false
	}
	return val.(V), true
}

// Store is [sync.Map.Store].
func (m *Map[K, V]) Store(key K, value V) { m.m.Store(key, value) }

// LoadOrStore is [sync.Map.LoadOrStore].
func (m *Map[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	v, loaded := m.m.LoadOrStore(key, value)
	return v.(V), loaded
}

// LoadAndDelete is [sync.Map.LoadAndDelete].
func (m *Map[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	v, loaded := m.m.LoadAndDelete(key)
	return v.(V), loaded
}

// Delete is [sync.Map.Delete].
func (m *Map[K, V]) Delete(key K) { m.m.Delete(key) }

// Range is [sync.Map.Range].
func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	m.m.Range(func(key, value any) bool {
		return f(key.(K), value.(V))
	})
}

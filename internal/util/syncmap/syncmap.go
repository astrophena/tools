// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package syncmap provides a generic version of [sync.Map].
package syncmap

import (
	"sync"
)

// Map is a generic concurrent map.
type Map[K comparable, V any] struct {
	m *sync.Map
}

// NewMap returns a new [Map].
func NewMap[K comparable, V any]() *Map[K, V] { return &Map[K, V]{m: &sync.Map{}} }

// Load returns the value stored in the map for a key, or nil if no value is
// present.
//
// The ok result indicates whether value was found in the map.
func (m *Map[K, V]) Load(key K) (value V, ok bool) {
	val, ok := m.m.Load(key)
	if !ok {
		return value, false
	}
	v, ok := val.(V)
	if !ok {
		// This should technically never happen, as we only store V type and delete
		// non-V types. However, handling it for safety.
		panic("syncmap: inconsistent map state: value is not of expected type")
	}
	return v, true
}

// Store sets the value for a key.
func (m *Map[K, V]) Store(key K, value V) {
	m.m.Store(key, value)
}

// LoadOrStore returns the existing value for the key if present.
// Otherwise, it stores and returns the given value.
// The loaded result is true if the value was loaded, false if stored.
func (m *Map[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	actualInterface, loaded := m.m.LoadOrStore(key, value)
	actual, ok := actualInterface.(V)
	if !ok {
		// This should technically never happen, as we only store V type
		// and delete non-V types. However, handling it for safety.
		panic("syncmap: inconsistent map state: value is not of expected type")
	}

	return actual, loaded
}

// LoadAndDelete deletes the value for a key, returning the previous value if
// any.  The loaded result reports whether the key was present.
func (m *Map[K, V]) LoadAndDelete(key K) (value V, loaded bool) {
	actualInterface, loaded := m.m.LoadAndDelete(key)
	if !loaded {
		return value, false
	}
	actual, ok := actualInterface.(V)
	if !ok {
		// This should technically never happen, as we only store V type and delete
		// non-V types. However, handling it for safety.
		panic("syncmap: inconsistent map state: value is not of expected type")
	}

	return actual, loaded
}

// Delete deletes the value for a key.
func (m *Map[K, V]) Delete(key K) {
	m.m.Delete(key)
}

// Range calls f sequentially for each key and value present in the map.
// If f returns false, range stops the iteration.
//
// The behavior of Range, Load, Store, and Delete is undefined if f modifies
// the map.
//
// Range does not necessarily correspond to any consistent snapshot of the Map's
// contents: no key will be visited more than once, but if the value for any key
// is stored or deleted concurrently, Range may reflect any mapping for that key
// from any point during the Range call.
func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	m.m.Range(func(key, value interface{}) bool {
		k, ok := key.(K)
		if !ok {
			// This should technically never happen, as we only store K type
			// and delete non-K types. However, handling it for safety.
			panic("syncmap: inconsistent map state: key is not of expected type")
		}
		v, ok := value.(V)
		if !ok {
			// This should technically never happen, as we only store V type
			// and delete non-V types. However, handling it for safety.
			panic("syncmap: inconsistent map state: value is not of expected type")
		}
		return f(k, v)
	})
}

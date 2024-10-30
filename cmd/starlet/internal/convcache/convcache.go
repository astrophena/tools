// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package convcache implements a Starlark module for caching conversations.
package convcache

import (
	"sync"
	"time"

	"go.astrophena.name/tools/internal/util/starlarkconv"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Module returns a Starlark module that exposes conversation caching functionality.
//
// This module provides three functions:
//
//   - get(chat_id: int) -> list: Retrieves the conversation history for the given chat ID.
//   - append(chat_id: int, message: str): Appends a new message to the conversation history.
//   - reset(chat_id: int): Clears the conversation history for the given chat ID.
//
// The chat ID is an integer representing a unique conversation identifier.
//
// The ttl argument specifies the time-to-live duration after which a cache entry will expire.
func Module(ttl time.Duration) *starlarkstruct.Module {
	m := &module{
		cache: make(map[int64]cacheEntry),
		ttl:   ttl,
	}
	return &starlarkstruct.Module{
		Name: "convcache",
		Members: starlark.StringDict{
			"get":    starlark.NewBuiltin("convcache.get", m.get),
			"append": starlark.NewBuiltin("convcache.append", m.append),
			"reset":  starlark.NewBuiltin("convcache.reset", m.reset),
		},
	}
}

type module struct {
	mu    sync.Mutex
	cache map[int64]cacheEntry
	ttl   time.Duration
}

type cacheEntry struct {
	value        []string
	lastAccessed time.Time
}

func (m *module) get(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var chatID int64
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "chat_id", &chatID); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.cache[chatID]
	if !ok {
		return starlark.NewList([]starlark.Value{}), nil
	}

	// Check if the entry has expired.
	if time.Since(entry.lastAccessed) > m.ttl {
		delete(m.cache, chatID)
		return starlark.NewList([]starlark.Value{}), nil
	}

	entry.lastAccessed = time.Now()
	m.cache[chatID] = entry

	return starlarkconv.ToValue(entry.value)
}

func (m *module) append(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var chatID int64
	var message string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "chat_id", &chatID, "message", &message); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.cache[chatID]
	if ok {
		entry.value = append(entry.value, message)
	} else {
		entry = cacheEntry{
			value:        []string{message},
			lastAccessed: time.Now(),
		}
	}

	m.cache[chatID] = entry

	return starlark.None, nil
}

func (m *module) reset(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var chatID int64
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "chat_id", &chatID); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.cache, chatID)

	return starlark.None, nil
}

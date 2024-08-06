// Package convcache implements a Starlark module for caching conversations.
package convcache

import (
	"sync"

	"go.astrophena.name/tools/internal/starlark/starconv"
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
func Module() *starlarkstruct.Module {
	m := &module{
		cache: make(map[int64][]string),
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
	cache map[int64][]string
}

func (m *module) get(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var chatID int64
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "chat_id", &chatID); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if history, ok := m.cache[chatID]; ok {
		return starconv.ToValue(history)
	}

	return starlark.NewList([]starlark.Value{}), nil
}

func (m *module) append(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var chatID int64
	var message string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "chat_id", &chatID, "message", &message); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.cache[chatID]; ok {
		m.cache[chatID] = append(m.cache[chatID], message)
	} else {
		m.cache[chatID] = []string{message}
	}

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

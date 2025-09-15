// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package kvcache

import (
	"context"

	"go.astrophena.name/tools/internal/store"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Module returns a Starlark module that exposes key-value caching functionality.
func Module(ctx context.Context, s store.Store) *starlarkstruct.Module {
	m := &module{
		ctx:   ctx,
		store: s,
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
	ctx   context.Context
	store store.Store
}

func (m *module) get(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key", &key); err != nil {
		return nil, err
	}
	return m.store.Get(m.ctx, key)
}

func (m *module) set(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		key   string
		value starlark.Value
	)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key", &key, "value", &value); err != nil {
		return nil, err
	}
	return starlark.None, m.store.Set(m.ctx, key, value)
}

// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package kvcache

import (
	"context"
	"encoding/json"
	"fmt"

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

	data, err := m.store.Get(m.ctx, key)
	if err != nil {
		return nil, err
	}

	if data == nil {
		return starlark.None, nil
	}

	return jsonToStarlark(data)
}

func (m *module) set(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		key   string
		value starlark.Value
	)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key", &key, "value", &value); err != nil {
		return nil, err
	}

	data, err := starlarkToJSON(value)
	if err != nil {
		return nil, err
	}

	return starlark.None, m.store.Set(m.ctx, key, data)
}

func starlarkToJSON(v starlark.Value) ([]byte, error) {
	switch v := v.(type) {
	case starlark.NoneType:
		return json.Marshal(nil)
	case starlark.Bool:
		return json.Marshal(bool(v))
	case starlark.Int:
		i, ok := v.Int64()
		if !ok {
			return nil, fmt.Errorf("int too large: %s", v.String())
		}
		return json.Marshal(i)
	case starlark.Float:
		return json.Marshal(float64(v))
	case starlark.String:
		return json.Marshal(v.GoString())
	case *starlark.List:
		list := make([]any, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			item, err := starlarkToJSON(v.Index(i))
			if err != nil {
				return nil, err
			}
			list = append(list, json.RawMessage(item))
		}
		return json.Marshal(list)
	case *starlark.Dict:
		dict := make(map[string]any, v.Len())
		for _, item := range v.Items() {
			k, v := item[0], item[1]
			key, ok := k.(starlark.String)
			if !ok {
				return nil, fmt.Errorf("dict key is not a string: %s", k.Type())
			}
			val, err := starlarkToJSON(v)
			if err != nil {
				return nil, err
			}
			dict[key.GoString()] = json.RawMessage(val)
		}
		return json.Marshal(dict)
	case starlark.Tuple:
		list := make([]any, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			item, err := starlarkToJSON(v.Index(i))
			if err != nil {
				return nil, err
			}
			list = append(list, json.RawMessage(item))
		}
		return json.Marshal(map[string]any{
			"__starlark_type__": "tuple",
			"values":            list,
		})
	case *starlarkstruct.Struct:
		names := v.AttrNames()
		dict := make(map[string]any, len(names))
		for _, name := range names {
			val, err := v.Attr(name)
			if err != nil {
				return nil, err
			}
			item, err := starlarkToJSON(val)
			if err != nil {
				return nil, err
			}
			dict[name] = json.RawMessage(item)
		}
		return json.Marshal(map[string]any{
			"__starlark_type__": "struct",
			"values":            dict,
		})
	default:
		return nil, fmt.Errorf("unsupported type: %s", v.Type())
	}
}

func jsonToStarlark(data []byte) (starlark.Value, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return jsonToStarlarkValue(v)
}

func jsonToStarlarkValue(v any) (starlark.Value, error) {
	switch v := v.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(v), nil
	case float64:
		if float64(int64(v)) == v {
			return starlark.MakeInt64(int64(v)), nil
		}
		return starlark.Float(v), nil
	case string:
		return starlark.String(v), nil
	case []any:
		list := make([]starlark.Value, 0, len(v))
		for _, item := range v {
			val, err := jsonToStarlarkValue(item)
			if err != nil {
				return nil, err
			}
			list = append(list, val)
		}
		return starlark.NewList(list), nil
	case map[string]any:
		if t, ok := v["__starlark_type__"].(string); ok {
			switch t {
			case "tuple":
				values, ok := v["values"].([]any)
				if !ok {
					return nil, fmt.Errorf("invalid tuple format")
				}
				list := make([]starlark.Value, 0, len(values))
				for _, item := range values {
					val, err := jsonToStarlarkValue(item)
					if err != nil {
						return nil, err
					}
					list = append(list, val)
				}
				return starlark.Tuple(list), nil
			case "struct":
				values, ok := v["values"].(map[string]any)
				if !ok {
					return nil, fmt.Errorf("invalid struct format")
				}
				tuples := make([]starlark.Tuple, 0, len(values))
				for k, item := range values {
					val, err := jsonToStarlarkValue(item)
					if err != nil {
						return nil, err
					}
					tuples = append(tuples, starlark.Tuple{starlark.String(k), val})
				}
				return starlarkstruct.FromKeywords(starlark.String("struct"), tuples), nil
			}
		}
		dict := starlark.NewDict(0)
		for k, item := range v {
			val, err := jsonToStarlarkValue(item)
			if err != nil {
				return nil, err
			}
			if err := dict.SetKey(starlark.String(k), val); err != nil {
				return nil, err
			}
		}
		return dict, nil
	default:
		return nil, fmt.Errorf("unsupported type: %T", v)
	}
}

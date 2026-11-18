// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package kvcache

import (
	"encoding/json"
	"errors"
	"fmt"

	"go.astrophena.name/tools/internal/cache"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Module returns the kvcache Starlark module backed by s.
func Module(s cache.Store) *starlarkstruct.Module {
	m := &module{
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
	store cache.Store
}

func (m *module) get(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key", &key); err != nil {
		return nil, err
	}

	data, err := m.store.Get(key)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return starlark.None, nil
	}

	return jsonToStarlark(data)
}

func (m *module) set(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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

	return starlark.None, m.store.Set(key, data)
}

// Tuples and structs carry a tag so they survive a JSON round trip.
const typeKey = "__starlark_type__"

func starlarkToJSON(v starlark.Value) ([]byte, error) {
	value, err := toJSONValue(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func toJSONValue(v starlark.Value) (any, error) {
	switch v := v.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(v), nil
	case starlark.Int:
		i, ok := v.Int64()
		if !ok {
			return nil, fmt.Errorf("int too large: %s", v)
		}
		return i, nil
	case starlark.Float:
		return float64(v), nil
	case starlark.String:
		return v.GoString(), nil
	case *starlark.List, starlark.Tuple:
		items := v.(starlark.Indexable)
		list := make([]any, items.Len())
		for i := range list {
			value, err := toJSONValue(items.Index(i))
			if err != nil {
				return nil, err
			}
			list[i] = value
		}
		if _, tuple := v.(starlark.Tuple); tuple {
			return map[string]any{typeKey: "tuple", "values": list}, nil
		}
		return list, nil
	case *starlark.Dict:
		dict := make(map[string]any, v.Len())
		for _, item := range v.Items() {
			key, ok := starlark.AsString(item[0])
			if !ok {
				return nil, fmt.Errorf("dict key is not a string: %s", item[0].Type())
			}
			value, err := toJSONValue(item[1])
			if err != nil {
				return nil, err
			}
			dict[key] = value
		}
		return dict, nil
	case *starlarkstruct.Struct:
		names := v.AttrNames()
		fields := make(map[string]any, len(names))
		for _, name := range names {
			attr, err := v.Attr(name)
			if err != nil {
				return nil, err
			}
			value, err := toJSONValue(attr)
			if err != nil {
				return nil, err
			}
			fields[name] = value
		}
		return map[string]any{typeKey: "struct", "values": fields}, nil
	default:
		return nil, fmt.Errorf("unsupported type: %s", v.Type())
	}
}

func jsonToStarlark(data []byte) (starlark.Value, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return fromJSONValue(v)
}

func fromJSONValue(v any) (starlark.Value, error) {
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
		list, err := fromJSONArray(v)
		if err != nil {
			return nil, err
		}
		return starlark.NewList(list), nil
	case map[string]any:
		return fromJSONObject(v)
	default:
		return nil, fmt.Errorf("unsupported type: %T", v)
	}
}

func fromJSONArray(values []any) ([]starlark.Value, error) {
	list := make([]starlark.Value, len(values))
	for i, item := range values {
		value, err := fromJSONValue(item)
		if err != nil {
			return nil, err
		}
		list[i] = value
	}
	return list, nil
}

func fromJSONObject(obj map[string]any) (starlark.Value, error) {
	switch obj[typeKey] {
	case "tuple":
		items, ok := obj["values"].([]any)
		if !ok {
			return nil, errors.New("invalid tuple format")
		}
		list, err := fromJSONArray(items)
		if err != nil {
			return nil, err
		}
		return starlark.Tuple(list), nil
	case "struct":
		fields, ok := obj["values"].(map[string]any)
		if !ok {
			return nil, errors.New("invalid struct format")
		}
		dict := make(starlark.StringDict, len(fields))
		for name, item := range fields {
			value, err := fromJSONValue(item)
			if err != nil {
				return nil, err
			}
			dict[name] = value
		}
		return starlarkstruct.FromStringDict(starlark.String("struct"), dict), nil
	default:
		dict := starlark.NewDict(len(obj))
		for key, item := range obj {
			value, err := fromJSONValue(item)
			if err != nil {
				return nil, err
			}
			if err := dict.SetKey(starlark.String(key), value); err != nil {
				return nil, err
			}
		}
		return dict, nil
	}
}

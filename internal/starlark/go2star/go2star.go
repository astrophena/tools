// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package go2star converts Go values to [starlark.Value].
package go2star

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
)

// To converts Go booleans, strings, numbers, times, slices, maps, and structs
// to Starlark values. It follows pointers and converts nil to None. Integral
// floats in the int64 range become Starlark integers.
//
// Structs become dictionaries of exported fields. The starlark tag takes
// precedence over the json tag and the Go field name.
func To(val any) (starlark.Value, error) {
	switch v := val.(type) {
	case nil:
		return starlark.None, nil
	case time.Time:
		return starlarktime.Time(v), nil
	}

	rv := reflect.ValueOf(val)
	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return starlark.None, nil
		}
		return To(rv.Elem().Interface())
	case reflect.Bool:
		return starlark.Bool(rv.Bool()), nil
	case reflect.String:
		return starlark.String(rv.String()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return starlark.MakeInt64(rv.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return starlark.MakeUint64(rv.Uint()), nil
	case reflect.Float32, reflect.Float64:
		fl := rv.Float()
		if canBeInt(fl) {
			return starlark.MakeInt64(int64(fl)), nil
		}
		return starlark.Float(fl), nil
	case reflect.Slice:
		list := make([]starlark.Value, rv.Len())
		for i := range rv.Len() {
			conv, err := To(rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			list[i] = conv
		}
		return starlark.NewList(list), nil
	case reflect.Map:
		return mapToDict(rv)
	case reflect.Struct:
		return structToDict(rv)
	default:
		return nil, fmt.Errorf("unsupported Go type: %T", val)
	}
}

func structToDict(rv reflect.Value) (starlark.Value, error) {
	dict := starlark.NewDict(rv.NumField())
	t := rv.Type()

	for i := range rv.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		fieldName, ok := field.Tag.Lookup("starlark")
		if !ok {
			if name, ok := field.Tag.Lookup("json"); ok {
				fieldName = strings.TrimSuffix(name, ",omitempty")
			} else {
				fieldName = field.Name
			}
		}

		value, err := To(rv.Field(i).Interface())
		if err != nil {
			return nil, fmt.Errorf("convert field %q: %w", fieldName, err)
		}
		if err := dict.SetKey(starlark.String(fieldName), value); err != nil {
			return nil, fmt.Errorf("set field %q: %w", fieldName, err)
		}
	}

	return dict, nil
}

func canBeInt(f float64) bool {
	// The upper bound is exclusive because float64 rounds MaxInt64 up to 2⁶³.
	return f >= math.MinInt64 && f < -float64(math.MinInt64) && f == math.Trunc(f)
}

func mapToDict(rv reflect.Value) (starlark.Value, error) {
	dict := starlark.NewDict(rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		key, err := To(iter.Key().Interface())
		if err != nil {
			return nil, fmt.Errorf("convert map key: %w", err)
		}

		val, err := To(iter.Value().Interface())
		if err != nil {
			return nil, fmt.Errorf("convert map value: %w", err)
		}

		if err := dict.SetKey(key, val); err != nil {
			return nil, fmt.Errorf("set map key: %w", err)
		}
	}

	return dict, nil
}

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

// To converts a Go value to a Starlark value.
//
// It supports the following Go types:
//
//   - nil: converted to [starlark.None]
//   - bool: converted to [starlark.Bool]
//   - string: converted to [starlark.String]
//   - int, int8, int16, int32, int64: converted to [starlark.Int]
//   - uint, uint8, uint16, uint32, uint64: converted to [starlark.Int]
//   - float32, float64: converted to [starlark.Float] or [starlark.Int] (if the value can be represented as an integer without loss of precision)
//   - [time.Time]: converted to [starlarktime.Time]
//   - slice: converted to [starlark.List] (elements are recursively converted)
//   - map: converted to [starlark.Dict] (keys and values are recursively converted)
//   - struct: converted to [starlark.Dict] (field names are used as keys, and field values are recursively converted). Unexported fields are ignored.
//
// If the Go value cannot be converted, an error is returned.
func To(val any) (starlark.Value, error) {
	if val == nil {
		return starlark.None, nil
	}

	rv := reflect.ValueOf(val)

	switch rv.Kind() {
	case reflect.Pointer:
		return To(rv.Elem())
	case reflect.Bool:
		return starlark.Bool(rv.Bool()), nil
	case reflect.String:
		return starlark.String(rv.String()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return starlark.MakeInt(int(rv.Int())), nil
	case reflect.Int64:
		return starlark.MakeInt64(rv.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return starlark.MakeUint(uint(rv.Uint())), nil
	case reflect.Uint64:
		return starlark.MakeUint64(rv.Uint()), nil
	case reflect.Float32, reflect.Float64:
		fl := rv.Float()
		if canBeInt(fl) {
			return starlark.MakeInt64(int64(fl)), nil
		}
		return starlark.Float(fl), nil
	case reflect.Slice:
		var list []starlark.Value
		for i := range rv.Len() {
			conv, err := To(rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			list = append(list, conv)
		}
		return starlark.NewList(list), nil
	case reflect.Map:
		return mapToDict(rv)
	case reflect.Struct:
		switch v := val.(type) {
		case time.Time:
			return starlarktime.Time(v), nil
		}
		return structToDict(rv)
	default:
		return nil, fmt.Errorf("unsupported Go type: %T", val)
	}
}

// structToDict converts Go struct to starlark.Value using reflection.
func structToDict(val reflect.Value) (starlark.Value, error) {
	dict := starlark.NewDict(val.NumField())
	structType := val.Type()

	for i := range val.NumField() {
		field := structType.Field(i)
		fieldValue := val.Field(i)

		// Skip unexported fields.
		if field.PkgPath != "" {
			continue
		}

		fieldName, ok := field.Tag.Lookup("starlark")
		if !ok {
			fieldName, ok = field.Tag.Lookup("json")
			fieldName = strings.TrimSuffix(fieldName, ",omitempty")
			if !ok {
				fieldName = field.Name
			}
		}

		fieldVal, err := To(fieldValue.Interface())
		if err != nil {
			return nil, fmt.Errorf("error converting field %s: %w", fieldName, err)
		}
		if err := dict.SetKey(starlark.String(fieldName), fieldVal); err != nil {
			return nil, fmt.Errorf("error setting field %s: %w", fieldName, err)
		}
	}

	return dict, nil
}

// canBeInt reports if the float can be converted to int without losing
// precision.
func canBeInt(f float64) bool {
	// Check if the float is within the representable range of int.
	if f < math.MinInt || f > math.MaxInt {
		return false
	}
	// Check if the float has a fractional part (i.e., it's not a whole number).
	if f != math.Trunc(f) {
		return false
	}
	return true
}

// mapToDict converts Go map to starlark.Value.
func mapToDict(rv reflect.Value) (starlark.Value, error) {
	dict := starlark.NewDict(rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		key, err := To(iter.Key().Interface())
		if err != nil {
			return nil, fmt.Errorf("error converting map key: %w", err)
		}

		val, err := To(iter.Value().Interface())
		if err != nil {
			return nil, fmt.Errorf("error converting map value: %w", err)
		}

		if err := dict.SetKey(key, val); err != nil {
			return nil, fmt.Errorf("error setting key-value in Starlark dict: %w", err)
		}
	}

	return dict, nil
}

// Package starconv implements Starlark value conversion.
//
// BUG: ToValue does not support arbitrary structs.
package starconv

import (
	"fmt"
	"math"
	"reflect"
	"time"

	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
)

// ToValue converts val to [starlark.Value].
func ToValue(val any) (starlark.Value, error) {
	rv := reflect.ValueOf(val)

	switch rv.Kind() {
	case reflect.Slice:
		// Handle Go slice conversion (recursive).
		var list []starlark.Value
		for i := range rv.Len() {
			conv, err := ToValue(rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			list = append(list, conv)
		}
		return starlark.NewList(list), nil
	case reflect.Map:
		// Handle nested Go map conversion (recursive).
		return mapToDict(rv)
	case reflect.Struct:
		// Handle Go struct conversion (recursive).
		switch v := val.(type) {
		case time.Time:
			return starlarktime.Time(v), nil
		}
		return structToDict(rv)
	default:
		switch v := val.(type) {
		case nil:
			return starlark.None, nil
		case bool:
			return starlark.Bool(v), nil
		case string:
			return starlark.String(v), nil
		case int:
			return starlark.MakeInt(v), nil
		case int8:
			return starlark.MakeInt(int(v)), nil
		case int16:
			return starlark.MakeInt(int(v)), nil
		case int32:
			return starlark.MakeInt(int(v)), nil
		case int64:
			return starlark.MakeInt64(v), nil
		case uint:
			return starlark.MakeUint(v), nil
		case uint8:
			return starlark.MakeUint(uint(v)), nil
		case uint16:
			return starlark.MakeUint(uint(v)), nil
		case uint32:
			return starlark.MakeUint(uint(v)), nil
		case uint64:
			return starlark.MakeUint64(v), nil
		case float32:
			if canBeInt(float64(v)) {
				return starlark.MakeInt64(int64(v)), nil
			}
			return starlark.Float(v), nil
		case float64:
			if canBeInt(v) {
				return starlark.MakeInt64(int64(v)), nil
			}
			return starlark.Float(v), nil
		default:
			return nil, fmt.Errorf("unsupported Go type: %T", val)
		}
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

		fieldName := field.Name
		fieldVal, err := ToValue(fieldValue.Interface())
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
		key, err := ToValue(iter.Key().Interface())
		if err != nil {
			return nil, fmt.Errorf("error converting map key: %w", err)
		}

		val, err := ToValue(iter.Value().Interface())
		if err != nil {
			return nil, fmt.Errorf("error converting map value: %w", err)
		}

		if err := dict.SetKey(key, val); err != nil {
			return nil, fmt.Errorf("error setting key-value in Starlark dict: %w", err)
		}
	}

	return dict, nil
}

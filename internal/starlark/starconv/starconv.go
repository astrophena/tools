// Package starconv implements Starlark value conversion.
//
// BUG: ToValue does not support arbitrary structs.
package starconv

import (
	"fmt"
	"math"
	"time"

	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
)

// ToValue converts val to [starlark.Value].
func ToValue(val any) (starlark.Value, error) {
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
	case time.Time:
		return starlarktime.Time(v), nil
	case []any:
		// Handle Go slice conversion (recursive).
		var list []starlark.Value
		for _, item := range v {
			conv, err := ToValue(item)
			if err != nil {
				return nil, err
			}
			list = append(list, conv)
		}
		return starlark.NewList(list), nil
	case map[string]any:
		// Handle nested Go map conversion (recursive).
		return mapToDict(v)
	default:
		return nil, fmt.Errorf("unsupported Go type: %T", val)
	}
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

// mapToDict converts map[string]any to starlark.Value.
func mapToDict(goMap map[string]any) (starlark.Value, error) {
	dict := starlark.NewDict(len(goMap))

	for key, value := range goMap {
		val, err := ToValue(value)
		if err != nil {
			return nil, fmt.Errorf("error converting Go value to Starlark: %w", err)
		}
		if err := dict.SetKey(starlark.String(key), val); err != nil {
			return nil, fmt.Errorf("error setting key-value in Starlark dict: %w", err)
		}
	}

	return dict, nil
}

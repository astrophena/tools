// Package envflag provides a wrapper around the standard flag package, allowing
// flags to be overridden by environment variables.
package envflag

import (
	"flag"
	"strconv"
)

// Type is a constraint that permits only types supported by envflag package.
type Type interface {
	int | int64 | float64 | bool | string
}

// Value sets up a flag with the given name, default value, and usage
// information.
//
// If the environment variable specified by envName is set, it overrides the
// flag's default value.
func Value[T Type](
	name, envName string, value T, usage string,
	fs *flag.FlagSet, getenv func(string) string,
) *T {
	var result T

	envValue := getenv(envName)
	if envValue != "" {
		// Try to parse the environment variable into the appropriate type.
		switch any(value).(type) {
		case int:
			if parsed, err := strconv.Atoi(envValue); err == nil {
				result = any(parsed).(T)
			} else {
				result = value
			}
		case int64:
			if parsed, err := strconv.ParseInt(envValue, 10, 64); err == nil {
				result = any(parsed).(T)
			} else {
				result = value
			}
		case float64:
			if parsed, err := strconv.ParseFloat(envValue, 64); err == nil {
				result = any(parsed).(T)
			} else {
				result = value
			}
		case bool:
			if parsed, err := strconv.ParseBool(envValue); err == nil {
				result = any(parsed).(T)
			} else {
				result = value
			}
		case string:
			result = any(envValue).(T)
		default:
			result = value
		}
	} else {
		result = value
	}

	usage += " Can be overridden by " + envName + " environment variable."

	fs.Var(newFlagValue(result, &result), name, usage)
	return &result
}

type flagValue[T any] struct {
	value *T
}

func newFlagValue[T any](defaultValue T, value *T) *flagValue[T] {
	*value = defaultValue
	return &flagValue[T]{value: value}
}

func (f *flagValue[T]) String() string {
	if f.value == nil {
		return ""
	}
	return toString(*f.value)
}

func (f *flagValue[T]) Set(s string) error {
	switch any(f.value).(type) {
	case *int:
		v, err := strconv.Atoi(s)
		if err != nil {
			return err
		}
		*f.value = any(v).(T)
	case *int64:
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		*f.value = any(v).(T)
	case *float64:
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		*f.value = any(v).(T)
	case *bool:
		v, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		*f.value = any(v).(T)
	case *string:
		*f.value = any(s).(T)
	default:
		return nil
	}
	return nil
}

func toString[T any](value T) string {
	switch v := any(value).(type) {
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case string:
		return v
	default:
		return ""
	}
}

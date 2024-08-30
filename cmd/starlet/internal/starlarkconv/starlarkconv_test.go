package starlarkconv

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
)

func TestToValue(t *testing.T) {
	cases := []struct {
		in   any
		want starlark.Value
	}{
		// Basic types.
		{nil, starlark.None},
		{true, starlark.True},
		{false, starlark.False},
		{"hello", starlark.String("hello")},
		{123, starlark.MakeInt(123)},
		{int8(42), starlark.MakeInt(42)},
		{int16(-1000), starlark.MakeInt(-1000)},
		{uint(123), starlark.MakeUint(123)},
		{uint8(255), starlark.MakeUint(255)},
		{uint16(65535), starlark.MakeUint(65535)},
		{int32(math.MaxInt32), starlark.MakeInt(math.MaxInt32)},
		{3.1415, starlark.Float(3.1415)},

		// Floats that can be represented as ints.
		{10.0, starlark.MakeInt(10)},
		{float32(-5.0), starlark.MakeInt(-5)},

		// Time.
		{time.Date(2023, 8, 15, 14, 30, 0, 0, time.UTC), starlarktime.Time(time.Date(2023, 8, 15, 14, 30, 0, 0, time.UTC))},

		// Slices.
		{[]int{1, 2, 3}, starlark.NewList([]starlark.Value{starlark.MakeInt(1), starlark.MakeInt(2), starlark.MakeInt(3)})},
		{[]string{"a", "b"}, starlark.NewList([]starlark.Value{starlark.String("a"), starlark.String("b")})},

		// Maps.
		{
			map[string]int{"one": 1},
			func() starlark.Value {
				dict := starlark.NewDict(1)
				dict.SetKey(starlark.String("one"), starlark.MakeInt(1))
				return dict
			}(),
		},

		// Structs.
		{
			struct {
				Name string
				Age  int
			}{"Bob", 30},
			func() starlark.Value {
				dict := starlark.NewDict(2)
				dict.SetKey(starlark.String("Name"), starlark.String("Bob"))
				dict.SetKey(starlark.String("Age"), starlark.MakeInt(30))
				return dict
			}(),
		},
		{
			struct {
				Name  string
				Age   int
				Slice []string
				Map   map[string]bool
				// We used only one member of the map because Go has randomized map order,
				// so it leads to flaky tests. We can sort map keys during conversion, but
				// I don't give a fuck.
			}{"Alice", 25, []string{"a", "b"}, map[string]bool{"x": true}},
			func() starlark.Value {
				dict := starlark.NewDict(4)
				dict.SetKey(starlark.String("Name"), starlark.String("Alice"))
				dict.SetKey(starlark.String("Age"), starlark.MakeInt(25))
				dict.SetKey(starlark.String("Slice"), starlark.NewList([]starlark.Value{starlark.String("a"), starlark.String("b")}))
				innerDict := starlark.NewDict(1)
				innerDict.SetKey(starlark.String("x"), starlark.True)
				dict.SetKey(starlark.String("Map"), innerDict)
				return dict
			}(),
		},

		// Nested structs.
		{
			struct {
				Person struct {
					Name string
				}
				City string
			}{struct{ Name string }{Name: "Eve"}, "London"},
			func() starlark.Value {
				dict := starlark.NewDict(2)
				innerDict := starlark.NewDict(1)
				innerDict.SetKey(starlark.String("Name"), starlark.String("Eve"))
				dict.SetKey(starlark.String("Person"), innerDict)
				dict.SetKey(starlark.String("City"), starlark.String("London"))
				return dict
			}(),
		},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%T", tc.in), func(t *testing.T) {
			got, err := ToValue(tc.in)
			if err != nil {
				t.Fatalf("ToValue(%v) returned error: %v", tc.in, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ToValue(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

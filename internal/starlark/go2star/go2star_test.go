// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package go2star

import (
	"math"
	"testing"
	"time"

	starlarktime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
)

type (
	customString string
	customBool   bool
)

func TestToValue(t *testing.T) {
	cases := map[string]struct {
		in   any
		want starlark.Value
	}{
		// Basic types.
		"nil": {
			nil,
			starlark.None,
		},
		"true": {
			true,
			starlark.True,
		},
		"false": {
			false,
			starlark.False,
		},
		"string": {
			"hello",
			starlark.String("hello"),
		},
		"int": {
			123,
			starlark.MakeInt(123),
		},
		"int8": {
			int8(42),
			starlark.MakeInt(42),
		},
		"int16": {
			int16(-1000),
			starlark.MakeInt(-1000),
		},
		"uint": {
			uint(123),
			starlark.MakeUint(123),
		},
		"uint8": {
			uint8(255),
			starlark.MakeUint(255),
		},
		"uint16": {
			uint16(65535),
			starlark.MakeUint(65535),
		},
		"int32": {
			int32(math.MaxInt32),
			starlark.MakeInt(math.MaxInt32),
		},
		"float": {
			3.1415,
			starlark.Float(3.1415),
		},
		"integral_float": {
			10.0,
			starlark.MakeInt(10),
		},
		"negative_integral_float32": {
			float32(-5.0),
			starlark.MakeInt(-5),
		},
		"max_int64_as_float": {
			float64(math.MaxInt64),
			starlark.Float(float64(math.MaxInt64)),
		},
		"min_int64_as_float": {
			float64(math.MinInt64),
			starlark.MakeInt64(math.MinInt64),
		},
		"positive_int64_limit_as_float": {
			-float64(math.MinInt64),
			starlark.Float(-float64(math.MinInt64)),
		},
		"math.NaN": {
			math.NaN(),
			starlark.Float(math.NaN()),
		},
		"math.Inf(1)": {
			math.Inf(1),
			starlark.Float(math.Inf(1)),
		},
		"math.Inf(-1)": {
			math.Inf(-1),
			starlark.Float(math.Inf(-1)),
		},
		"time.Time": {
			time.Date(2023, 8, 15, 14, 30, 0, 0, time.UTC),
			starlarktime.Time(time.Date(2023, 8, 15, 14, 30, 0, 0, time.UTC)),
		},
		"slice_int": {
			[]int{1, 2, 3},
			starlark.NewList([]starlark.Value{starlark.MakeInt(1), starlark.MakeInt(2), starlark.MakeInt(3)}),
		},
		"slice_string": {
			[]string{"a", "b"},
			starlark.NewList([]starlark.Value{starlark.String("a"), starlark.String("b")}),
		},
		"map": {
			map[string]int{"one": 1},
			func() starlark.Value {
				dict := starlark.NewDict(1)
				dict.SetKey(starlark.String("one"), starlark.MakeInt(1))
				return dict
			}(),
		},
		"struct": {
			struct {
				Name string
				Age  int
			}{
				"Bob",
				30,
			},
			func() starlark.Value {
				dict := starlark.NewDict(2)
				dict.SetKey(starlark.String("Name"), starlark.String("Bob"))
				dict.SetKey(starlark.String("Age"), starlark.MakeInt(30))
				return dict
			}(),
		},
		"struct_nested_values": {
			struct {
				Name  string
				Age   int
				Slice []string
				Map   map[string]bool
			}{
				"Alice",
				25,
				[]string{"a", "b"},
				map[string]bool{"x": true},
			},
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
		"nested_structs": {
			struct {
				Person struct {
					Name string
				}
				City string
			}{
				struct {
					Name string
				}{
					Name: "Eve",
				},
				"London",
			},
			func() starlark.Value {
				dict := starlark.NewDict(2)
				innerDict := starlark.NewDict(1)
				innerDict.SetKey(starlark.String("Name"), starlark.String("Eve"))
				dict.SetKey(starlark.String("Person"), innerDict)
				dict.SetKey(starlark.String("City"), starlark.String("London"))
				return dict
			}(),
		},
		"struct_with_starlark_tags": {
			struct {
				Name string `starlark:"name"`
				Age  int    `starlark:"age"`
			}{
				"Bob",
				30,
			},
			func() starlark.Value {
				dict := starlark.NewDict(2)
				dict.SetKey(starlark.String("name"), starlark.String("Bob"))
				dict.SetKey(starlark.String("age"), starlark.MakeInt(30))
				return dict
			}(),
		},
		"struct_with_starlark_json_tags": {
			struct {
				A string `starlark:"a" json:"aa"`
				B int    `starlark:"b" json:"bb"`
			}{
				"foo",
				1,
			},
			func() starlark.Value {
				dict := starlark.NewDict(2)
				dict.SetKey(starlark.String("a"), starlark.String("foo"))
				dict.SetKey(starlark.String("b"), starlark.MakeInt(1))
				return dict
			}(),
		},
		"struct_with_json_tags": {
			struct {
				A string `json:"a"`
				B int    `json:"b"`
			}{
				"foo",
				1,
			},
			func() starlark.Value {
				dict := starlark.NewDict(2)
				dict.SetKey(starlark.String("a"), starlark.String("foo"))
				dict.SetKey(starlark.String("b"), starlark.MakeInt(1))
				return dict
			}(),
		},
		"pointer": {
			new(42),
			starlark.MakeInt(42),
		},
		"nil_pointer": {
			(*int)(nil),
			starlark.None,
		},
		"customString": {
			customString("foobar"),
			starlark.String("foobar"),
		},
		"customBool": {
			customBool(true),
			starlark.Bool(true),
		},
		"struct_with_custom_types": {
			struct {
				Type   customString `starlark:"type"`
				Active customBool   `starlark:"active"`
			}{
				Type:   customString("foo"),
				Active: customBool(false),
			},
			func() starlark.Value {
				dict := starlark.NewDict(2)
				dict.SetKey(starlark.String("type"), starlark.String("foo"))
				dict.SetKey(starlark.String("active"), starlark.Bool(false))
				return dict
			}(),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := To(tc.in)
			if err != nil {
				t.Fatalf("To(%v) returned error: %v", tc.in, err)
			}

			if f, ok := tc.in.(float64); ok && math.IsNaN(f) {
				if gotFloat, ok := got.(starlark.Float); ok && math.IsNaN(float64(gotFloat)) {
					return
				}
				t.Errorf("To(%v) = %v, want NaN", tc.in, got)
				return
			}

			eq, err := starlark.Equal(got, tc.want)
			if err != nil {
				t.Errorf("To(%v) = %v, got error when comparing: %v", tc.in, got, err)
			}
			if !eq {
				t.Errorf("To(%v) = %v (%T), want %v (%T)", tc.in, got, got, tc.want, tc.want)
			}
		})
	}
}

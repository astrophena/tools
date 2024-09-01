// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package convcache

import (
	"testing"
	"time"

	"go.starlark.net/starlark"
)

func TestConvCache(t *testing.T) {
	mod := Module(time.Hour)

	thread := &starlark.Thread{}

	// Test append function.
	_, err := mod.Members["append"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{starlark.MakeInt64(1), starlark.String("Hello, world!")}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Test get function.
	result, err := mod.Members["get"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{starlark.MakeInt64(1)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantGet := starlark.NewList([]starlark.Value{starlark.String("Hello, world!")})
	if equal, err := starlark.Equal(result, wantGet); !equal {
		t.Errorf("get(1) = %v, want %v", result, wantGet)
	} else if err != nil {
		t.Errorf("got an error when comparing result and wantGot: %v", err)
	}

	// Test reset function.
	_, err = mod.Members["reset"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{starlark.MakeInt64(1)}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestConvCacheTTL(t *testing.T) {
	mod := Module(time.Nanosecond)

	thread := &starlark.Thread{}

	// Append an entry there.
	_, err := mod.Members["append"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{starlark.MakeInt64(1), starlark.String("Hello, world!")}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Test TTL expiration (without using time.Sleep).
	// Since the TTL is very short (time.Nanosecond), the entry should already be expired.
	result, err := mod.Members["get"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{starlark.MakeInt64(1)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantExpired := starlark.NewList([]starlark.Value{})
	if equal, err := starlark.Equal(result, wantExpired); !equal {
		t.Errorf("get(1) after TTL expired = %v, want %v", result, wantExpired)
	} else if err != nil {
		t.Errorf("got an error when comparing result and wantExpired: %v", err)
	}
}

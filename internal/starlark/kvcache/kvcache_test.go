// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package kvcache

import (
	"testing"
	"testing/synctest"
	"time"

	"go.astrophena.name/tools/internal/store"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func TestSetGet(t *testing.T) {
	s := store.NewMemStore(t.Context(), time.Minute)
	mod := Module(t.Context(), s)
	thread := &starlark.Thread{Name: t.Name()}

	key1 := starlark.String("mykey")
	value1 := starlark.String("myvalue")
	key2 := starlark.String("anotherkey")
	value2 := starlark.MakeInt(123)

	// Test set.
	_, err := mod.Members["set"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{key1, value1}, nil)
	if err != nil {
		t.Fatalf("set(%q, %q) failed: %v", key1, value1, err)
	}
	_, err = mod.Members["set"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{key2, value2}, nil)
	if err != nil {
		t.Fatalf("set(%q, %v) failed: %v", key2, value2, err)
	}

	// Test get existing key.
	got, err := mod.Members["get"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{key1}, nil)
	if err != nil {
		t.Fatalf("get(%q) failed: %v", key1, err)
	}
	if eq, _ := starlark.Equal(got, value1); !eq {
		t.Errorf("get(%q) = %v, want %v", key1, got, value1)
	}

	got, err = mod.Members["get"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{key2}, nil)
	if err != nil {
		t.Fatalf("get(%q) failed: %v", key2, err)
	}
	if eq, _ := starlark.Equal(got, value2); !eq {
		t.Errorf("get(%q) = %v, want %v", key2, got, value2)
	}

	// Test get non-existent key.
	nonExistentKey := starlark.String("nokey")
	got, err = mod.Members["get"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{nonExistentKey}, nil)
	if err != nil {
		t.Fatalf("get(%q) failed: %v", nonExistentKey, err)
	}
	if got != starlark.None {
		t.Errorf("get(%q) = %v, want None", nonExistentKey, got)
	}

	// Test overwrite value.
	newValue1 := starlark.Bool(true)
	_, err = mod.Members["set"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{key1, newValue1}, nil)
	if err != nil {
		t.Fatalf("set(%q, %v) failed: %v", key1, newValue1, err)
	}
	got, err = mod.Members["get"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{key1}, nil)
	if err != nil {
		t.Fatalf("get(%q) after overwrite failed: %v", key1, err)
	}
	if eq, _ := starlark.Equal(got, newValue1); !eq {
		t.Errorf("get(%q) after overwrite = %v, want %v", key1, got, newValue1)
	}
}

func TestTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ttl := 50 * time.Millisecond
		s := store.NewMemStore(t.Context(), ttl)
		mod := Module(t.Context(), s)
		thread := &starlark.Thread{Name: t.Name()}

		key := starlark.String("expiring_key")
		value := starlark.String("expiring_value")

		// Set an entry.
		_, err := mod.Members["set"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{key, value}, nil)
		if err != nil {
			t.Fatalf("set(%q, %q) failed: %v", key, value, err)
		}

		// Get immediately, should exist.
		got, err := mod.Members["get"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{key}, nil)
		if err != nil {
			t.Fatalf("get(%q) immediately failed: %v", key, err)
		}
		if eq, _ := starlark.Equal(got, value); !eq {
			t.Errorf("get(%q) immediately = %v, want %v", key, got, value)
		}

		// Wait for longer than TTL.
		time.Sleep(ttl + 20*time.Millisecond)

		// Get again, should be None (expired).
		got, err = mod.Members["get"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{key}, nil)
		if err != nil {
			t.Fatalf("get(%q) after TTL failed: %v", key, err)
		}
		if got != starlark.None {
			t.Errorf("get(%q) after TTL = %v, want None", key, got)
		}
	})
}

func TestTTL_ResetOnGet(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ttl := 100 * time.Millisecond
		s := store.NewMemStore(t.Context(), ttl)
		mod := Module(t.Context(), s)
		thread := &starlark.Thread{Name: "TestKVCache_TTL_ResetOnGet"}

		key := starlark.String("reset_key")
		value := starlark.String("reset_value")

		// Set an entry.
		_, err := mod.Members["set"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{key, value}, nil)
		if err != nil {
			t.Fatalf("set(%q, %q) failed: %v", key, value, err)
		}

		// Wait for less than TTL.
		time.Sleep(ttl / 2)

		// Get the entry, should exist and reset the timer.
		got, err := mod.Members["get"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{key}, nil)
		if err != nil {
			t.Fatalf("get(%q) before expiry failed: %v", key, err)
		}
		if eq, _ := starlark.Equal(got, value); !eq {
			// Fatal because next steps depend on this.
			t.Fatalf("get(%q) before expiry = %v, want %v", key, got, value)
		}

		// Wait again for less than TTL (but total time > original TTL).
		time.Sleep(ttl / 2)

		// Get again, should still exist because the first get reset the timer.
		got, err = mod.Members["get"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{key}, nil)
		if err != nil {
			t.Fatalf("get(%q) after reset failed: %v", key, err)
		}
		if eq, _ := starlark.Equal(got, value); !eq {
			t.Errorf("get(%q) after reset = %v, want %v (expected TTL reset)", key, got, value)
		}

		// Wait for longer than TTL after the last get.
		time.Sleep(ttl + 20*time.Millisecond)

		// Get again, should now be expired.
		got, err = mod.Members["get"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{key}, nil)
		if err != nil {
			t.Fatalf("get(%q) finally failed: %v", key, err)
		}
		if got != starlark.None {
			t.Errorf("get(%q) finally = %v, want None (expected eventual expiry)", key, got)
		}
	})
}

func TestSerialization(t *testing.T) {
	values := []starlark.Value{
		starlark.None,
		starlark.Bool(true),
		starlark.MakeInt(123),
		starlark.Float(123.456),
		starlark.String("hello"),
		starlark.NewList([]starlark.Value{starlark.String("a"), starlark.MakeInt(1)}),
		starlark.Tuple{starlark.String("b"), starlark.MakeInt(2)},
		func() *starlark.Dict {
			d := starlark.NewDict(1)
			d.SetKey(starlark.String("c"), starlark.MakeInt(3))
			return d
		}(),
		starlarkstruct.FromStringDict(starlark.String("struct"), starlark.StringDict{
			"d": starlark.MakeInt(4),
		}),
	}

	for _, v := range values {
		t.Run(v.Type(), func(t *testing.T) {
			data, err := starlarkToJSON(v)
			if err != nil {
				t.Fatal(err)
			}
			got, err := jsonToStarlark(data)
			if err != nil {
				t.Fatal(err)
			}
			eq, err := starlark.Equal(v, got)
			if err != nil {
				t.Fatal(err)
			}
			if !eq {
				t.Errorf("got %s, want %s", got.String(), v.String())
			}
		})
	}
}

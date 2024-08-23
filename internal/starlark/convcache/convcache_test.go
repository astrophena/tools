package convcache

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.starlark.net/starlark"
)

func TestConvCache(t *testing.T) {
	// Initialize the module with some initial data.
	mod, export := Module(map[int64][]string{
		1: {"Hello!", "How are you?"},
	}, time.Hour) // Use a long TTL to avoid expiration during the test

	// Create a Starlark thread for testing.
	thread := &starlark.Thread{}

	// Test get function.
	result, err := mod.Members["get"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{starlark.MakeInt64(1)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantGet := starlark.NewList([]starlark.Value{starlark.String("Hello!"), starlark.String("How are you?")})
	if equal, err := starlark.Equal(result, wantGet); !equal {
		t.Errorf("get(1) = %v, want %v", result, wantGet)
	} else if err != nil {
		t.Errorf("got an error when comparing result and wantGot: %v", err)
	}

	// Test append function.
	_, err = mod.Members["append"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{starlark.MakeInt64(1), starlark.String("I'm doing well!")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantAppend := []string{"Hello!", "How are you?", "I'm doing well!"}
	if diff := cmp.Diff(export()[1], wantAppend); diff != "" {
		t.Errorf("append(1, \"I'm doing well!\") mismatch (-want +got):\n%s", diff)
	}

	// Test reset function.
	_, err = mod.Members["reset"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{starlark.MakeInt64(1)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := export()[1]; ok {
		t.Error("reset(1) did not clear the cache")
	}
}

func TestConvCacheTTL(t *testing.T) {
	// Initialize the module with some initial data and a short TTL.
	mod, _ := Module(map[int64][]string{
		1: {"Hello!", "How are you?"},
	}, time.Nanosecond)

	// Create a Starlark thread for testing.
	thread := &starlark.Thread{}

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

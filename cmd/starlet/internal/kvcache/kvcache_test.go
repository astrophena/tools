// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package kvcache

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.starlark.net/starlark"
)

func TestKVCache_SetGet(t *testing.T) {
	// Use a reasonable TTL that won't expire during the test.
	mod, _ := Module(time.Minute)
	thread := &starlark.Thread{Name: "TestKVCache_SetGet"}

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

func TestKVCache_TTL(t *testing.T) {
	ttl := 50 * time.Millisecond
	mod, _ := Module(ttl)
	thread := &starlark.Thread{Name: "TestKVCache_TTL"}

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
}

func TestKVCache_TTL_ResetOnGet(t *testing.T) {
	ttl := 100 * time.Millisecond
	mod, _ := Module(ttl)
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
}

func TestKVCache_ServeDebug(t *testing.T) {
	ttl := 5 * time.Minute
	mod, handler := Module(ttl)
	thread := &starlark.Thread{Name: "TestKVCache_ServeDebug"}

	// Add some data.
	key1 := starlark.String("mykey1")
	value1 := starlark.String("value_one")
	key2 := starlark.String("akey2") // start with 'a' to test sorting
	value2 := starlark.MakeInt(999)

	_, err := mod.Members["set"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{key1, value1}, nil)
	if err != nil {
		t.Fatalf("set failed: %v", err)
	}
	_, err = mod.Members["set"].(*starlark.Builtin).CallInternal(thread, starlark.Tuple{key2, value2}, nil)
	if err != nil {
		t.Fatalf("set failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/debug/kvcache", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	// Check body content (basic checks).
	body := rr.Body.String()
	if !strings.Contains(body, "<title>KV Cache Debug</title>") {
		t.Errorf("response body does not contain expected title")
	}

	// Check TTL display.
	if !strings.Contains(body, ttl.String()) {
		t.Errorf("response body does not contain expected TTL: %s", ttl.String())
	}

	// Check if keys and values are present (order matters due to sorting).
	if !strings.Contains(body, "<code>akey2</code>") {
		t.Errorf("response body does not contain key %q", key2.GoString())
	}
	if !strings.Contains(body, "<code>999</code>") {
		t.Errorf("response body does not contain value %q", value2.String())
	}
	if !strings.Contains(body, "<code>mykey1</code>") {
		t.Errorf("response body does not contain key %q", key1.GoString())
	}
	if !strings.Contains(body, `<code>"value_one"</code>`) {
		t.Errorf("response body does not contain value %q", value1.String())
	}

	t.Log(body)

	// Check sorting implicitly by order in HTML (akey2 should appear before
	// mykey1).
	idxKey2 := strings.Index(body, "<code>akey2</code>")
	idxKey1 := strings.Index(body, "<code>mykey1</code>")
	if idxKey1 == -1 || idxKey2 == -1 || idxKey2 >= idxKey1 {
		t.Errorf("keys do not appear sorted in response body ('akey2' should come before 'mykey1')")
	}
}

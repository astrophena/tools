// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package cache

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestMemStore(t *testing.T) {
	t.Parallel()
	testStore(t, NewMemStore(t.Context(), time.Hour))
}

func TestJSONFile(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	path := filepath.Join(t.TempDir(), "store.json")
	s, err := NewJSONFile(ctx, path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	testStore(t, s)

	if err := s.Set("nil", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("nil"); err != nil {
		t.Fatal(err)
	}
	stats := s.Stats()
	if stats.Expired != 0 {
		t.Errorf("expired entries = %d, want 0", stats.Expired)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	cancel()

	reopened, err := NewJSONFile(t.Context(), path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Stats().TotalGets; got != stats.Gets {
		t.Errorf("gets after reopening = %d, want %d", got, stats.Gets)
	}
	got, err := reopened.Get("key2")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "123" {
		t.Errorf("reopened value = %q, want %q", got, "123")
	}
}

func testStore(t *testing.T, s Store) {
	t.Helper()

	value1 := []byte(`"value1"`)
	value2 := []byte(`123`)

	if err := s.Set("key1", value1); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("key2", value2); err != nil {
		t.Fatal(err)
	}

	v, err := s.Get("key1")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(v, value1) {
		t.Errorf("got %q, want %q", v, value1)
	}

	v, err = s.Get("key2")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(v, value2) {
		t.Errorf("got %q, want %q", v, value2)
	}

	v, err = s.Get("key3")
	if err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Errorf("got %q, want nil", v)
	}
}

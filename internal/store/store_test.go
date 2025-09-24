// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package store

import (
	"bytes"
	"context"
	"testing"
	"time"

	_ "github.com/tailscale/sqlite"
)

func TestMemStore(t *testing.T) {
	s := NewMemStore(t.Context(), 50*time.Millisecond)
	testStore(t, s)
}

func TestSQLiteStore(t *testing.T) {
	s, err := NewSQLiteStore(t.Context(), "file:/store-test?vfs=memdb", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Clean up the table before running the test.
	if _, err := s.db.ExecContext(t.Context(), "DELETE FROM kv"); err != nil {
		t.Fatal(err)
	}

	testStore(t, s)
}

func testStore(t *testing.T, s Store) {
	ctx := context.Background()

	value1 := []byte(`"value1"`)
	value2 := []byte(`123`)

	// Test Set and Get.
	if err := s.Set(ctx, "key1", value1); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(ctx, "key2", value2); err != nil {
		t.Fatal(err)
	}

	v, err := s.Get(ctx, "key1")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(v, value1) {
		t.Errorf("got %q, want %q", v, value1)
	}

	v, err = s.Get(ctx, "key2")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(v, value2) {
		t.Errorf("got %q, want %q", v, value2)
	}

	// Test Get non-existent key.
	v, err = s.Get(ctx, "key3")
	if err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Errorf("got %q, want nil", v)
	}
}

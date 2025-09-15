// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package store

import (
	"context"
	"os"
	"testing"
	"time"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func TestMemStore(t *testing.T) {
	s := NewMemStore(t.Context(), 50*time.Millisecond)
	testStore(t, s)
}

func TestPostgresStore(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	ctx := context.Background()
	s, err := NewPostgresStore(ctx, databaseURL, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Clean up the table before running the test.
	if _, err := s.conn.Exec(ctx, "DELETE FROM kv"); err != nil {
		t.Fatal(err)
	}

	testStore(t, s)
}

func testStore(t *testing.T, s Store) {
	ctx := context.Background()

	// Test Set and Get.
	if err := s.Set(ctx, "key1", starlark.String("value1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(ctx, "key2", starlark.MakeInt(123)); err != nil {
		t.Fatal(err)
	}

	v, err := s.Get(ctx, "key1")
	if err != nil {
		t.Fatal(err)
	}
	if v.(starlark.String) != "value1" {
		t.Errorf("got %q, want %q", v, "value1")
	}

	v, err = s.Get(ctx, "key2")
	if err != nil {
		t.Fatal(err)
	}
	if v.(starlark.Int).String() != "123" {
		t.Errorf("got %q, want %q", v, "123")
	}

	// Test Get non-existent key.
	v, err = s.Get(ctx, "key3")
	if err != nil {
		t.Fatal(err)
	}
	if v != starlark.None {
		t.Errorf("got %q, want None", v)
	}
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

// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package filelock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireConflict(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".run.lock")
	first, err := Acquire(path, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := first.Release(); err != nil {
			t.Fatal(err)
		}
	})

	_, err = Acquire(path, "")
	if !errors.Is(err, ErrAlreadyLocked) {
		t.Fatalf("want %v, got %v", ErrAlreadyLocked, err)
	}
}

func TestAcquireWritesPayload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".run.lock")
	lock, err := Acquire(path, "pid=123\n")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Release(); err != nil {
			t.Fatal(err)
		}
	})

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "pid=123\n" {
		t.Fatalf("unexpected payload: %q", string(payload))
	}
}

func TestIsLockedLifecycle(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".run.lock")
	if IsLocked(path) {
		t.Fatal("expected unlocked file")
	}

	lock, err := Acquire(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if !IsLocked(path) {
		t.Fatal("expected file to be locked")
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if IsLocked(path) {
		t.Fatal("expected file to be unlocked")
	}
}

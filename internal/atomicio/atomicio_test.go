// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package atomicio

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
)

func TestWriteFile(t *testing.T) {
	t.Parallel()

	t.Run("new file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		file := filepath.Join(dir, "test.txt")
		data := []byte("hello")

		if err := WriteFile(file, data, 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, string(got), string(data))

		backups, err := filepath.Glob(file + ".*.bak")
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, len(backups), 0)
	})

	t.Run("overwrite", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		file := filepath.Join(dir, "test.txt")
		data1 := []byte("hello")
		data2 := []byte("world")

		if err := WriteFile(file, data1, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := WriteFile(file, data2, 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, string(got), string(data2))

		backups, err := filepath.Glob(file + ".*.bak")
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, len(backups), 1)

		backupData, err := os.ReadFile(backups[0])
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, string(backupData), string(data1))
	})

	t.Run("prune", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		file := filepath.Join(dir, "test.txt")

		// Create more than maxBackups files.
		for i := 0; i < maxBackups+2; i++ {
			data := []byte{byte(i)}
			if err := WriteFile(file, data, 0o644); err != nil {
				t.Fatal(err)
			}
			// Sleep to ensure unique backup timestamps.
			time.Sleep(2 * time.Millisecond)
		}

		backups, err := filepath.Glob(file + ".*.bak")
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, len(backups), maxBackups)
	})
}

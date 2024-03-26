// Package testutil contains common testing helpers.
package testutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/txtar"
)

// AssertEqual compares two values and if they differ, fails the test and
// prints the difference between them.
func AssertEqual(t *testing.T, want, got any) {
	t.Helper()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("(-want +got):\n%s", diff)
	}
}

// ExtractTxtar extracts a txtar archive to dir.
func ExtractTxtar(t *testing.T, ar *txtar.Archive, dir string) {
	for _, file := range ar.Files {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(file.Name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file.Name), file.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// BuildTxtar constructs a txtar archive from contents of dir.
func BuildTxtar(t *testing.T, dir string) []byte {
	ar := new(txtar.Archive)

	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		ar.Files = append(ar.Files, txtar.File{
			Name: d.Name(),
			Data: b,
		})

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	return txtar.Format(ar)
}

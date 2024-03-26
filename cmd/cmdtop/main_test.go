package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"go.astrophena.name/tools/internal/testutil"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestCount(t *testing.T) {
	in, err := os.ReadFile(filepath.Join("testdata", "history"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := count(bytes.NewReader(in), 10)
	if err != nil {
		t.Fatal(err)
	}

	golden := filepath.Join("testdata", "top.golden")
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatalf("unable to write golden file %q: %v", golden, err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, want, []byte(got))
}

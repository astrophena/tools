package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"go.astrophena.name/tools/internal/testutil"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestCount(t *testing.T) {
	testutil.RunGolden(t, filepath.Join("testdata", "history"), func(t *testing.T, match string) []byte {
		f, err := os.Open(match)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()

		got, err := count(f, 10)
		if err != nil {
			t.Fatal(err)
		}

		return got
	}, *update)
}

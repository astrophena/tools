package main

import (
	"flag"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestRename(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, tc string) []byte {
		tca, err := txtar.ParseFile(tc)
		if err != nil {
			t.Fatal(err)
		}

		dir := t.TempDir()
		testutil.ExtractTxtar(t, tca, dir)

		if err := rename(dir, 1, t.Logf); err != nil {
			t.Fatal(err)
		}

		return testutil.BuildTxtar(t, dir)
	}, *update)
}

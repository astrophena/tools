package main

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"go.astrophena.name/tools/internal/testutil"

	"golang.org/x/tools/txtar"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}

func TestLookup(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, tc string) []byte {
		tca, err := txtar.ParseFile(tc)
		if err != nil {
			t.Fatal(err)
		}

		dir := t.TempDir()
		testutil.ExtractTxtar(t, tca, dir)

		dups, err := lookup(dir)
		if err != nil {
			t.Fatal(err)
		}

		return []byte(fmt.Sprintf("%+v", dups))
	}, *update)
}

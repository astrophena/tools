package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/tools/internal/testutil"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/txtar"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}

func TestLookup(t *testing.T) {
	cases, err := filepath.Glob("testdata/*.txtar")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range cases {
		tcName := strings.TrimSuffix(tc, filepath.Ext(tc))
		tcName = strings.TrimPrefix(tcName, "testdata"+string(filepath.Separator))

		t.Run(tcName, func(t *testing.T) {
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

			got := []byte(fmt.Sprintf("%+v", dups))

			golden := filepath.Join("testdata", tcName+".golden")
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatalf("unable to write golden file %q: %v", golden, err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("(-want +got): \n%s", diff)
			}
		})
	}
}

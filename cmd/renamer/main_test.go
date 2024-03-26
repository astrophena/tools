package main

import (
	"flag"
	"log"
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

func TestRename(t *testing.T) {
	cases, err := filepath.Glob("testdata/*.txtar")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range cases {
		tcName := strings.TrimSuffix(tc, filepath.Ext(tc))
		tcName = strings.TrimPrefix(tcName, "testdata"+string(filepath.Separator))

		t.Run(tcName, func(t *testing.T) {
			logf = t.Logf
			t.Cleanup(func() {
				logf = log.Printf
			})

			tca, err := txtar.ParseFile(tc)
			if err != nil {
				t.Fatal(err)
			}

			dir := t.TempDir()
			testutil.ExtractTxtar(t, tca, dir)

			if err := rename(dir, 1); err != nil {
				t.Fatal(err)
			}

			got := testutil.BuildTxtar(t, dir)

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

func TestAskForConfirmation(t *testing.T) {
	cases := map[string]struct {
		in   string
		want bool
	}{
		"yes response should return true": {
			in:   "yes\n",
			want: true,
		},
		"no response should return false": {
			in:   "no\n",
			want: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := askForConfirmation(strings.NewReader(tc.in)); got != tc.want {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

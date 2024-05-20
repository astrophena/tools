package main

import (
	"flag"
	"log"
	"os"
	"strings"
	"testing"

	"go.astrophena.name/tools/internal/testutil"
	"go.astrophena.name/tools/internal/txtar"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}

func TestRename(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, tc string) []byte {
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

		return testutil.BuildTxtar(t, dir)
	}, *update)
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

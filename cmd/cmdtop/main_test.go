// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/cli/clitest"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestRun(t *testing.T) {
	t.Parallel()

	clitest.Run(t, func(t *testing.T) cli.AppFunc {
		return cli.AppFunc(run)
	}, map[string]clitest.Case[cli.AppFunc]{
		"version": {
			Args:         []string{"-version"},
			WantErr:      cli.ErrExitVersion,
			WantInStderr: "cmdtop",
		},
		"invalid number of commands": {
			Args:    []string{"foo"},
			WantErr: errInvalidNum,
		},
		"reads from HISTFILE": {
			Env: map[string]string{
				"HISTFILE": filepath.Join("testdata", "history"),
			},
			WantInStdout: read(filepath.Join("testdata", "history.golden")),
		},
	})
}

func read(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestCount(t *testing.T) {
	t.Parallel()

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

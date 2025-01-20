// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"flag"
	"io/fs"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/cli/clitest"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestRun(t *testing.T) {
	clitest.Run[*app](t, func(t *testing.T) *app {
		return &app{}
	}, map[string]clitest.Case[*app]{
		"prints usage with help flag": {
			Args:    []string{"-h"},
			WantErr: flag.ErrHelp,
		},
		"version": {
			Args:    []string{"-version"},
			WantErr: cli.ErrExitVersion,
		},
		"nonexistent file": {
			Args:    []string{"nonexistent.md", "foo.md"},
			WantErr: fs.ErrNotExist,
		},
		"prints to standard out": {
			Args:         []string{"testdata/full.md"},
			WantInStdout: "Copied from",
		},
		"no files passed": {
			Args:    []string{},
			WantErr: cli.ErrInvalidArgs,
		},
		"rewrites the file in place": {
			Args: []string{"-w", "testdata/hello.md"},
		},
	})
}

func TestFormat(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.md", func(t *testing.T, match string) []byte {
		a := &app{
			lineLength: 120,
		}
		b, err := a.format(match)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}, *update)
}

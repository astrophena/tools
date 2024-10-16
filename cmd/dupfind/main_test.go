// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.astrophena.name/tools/internal/cli"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestRun(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args               []string
		wantErr            error
		wantNothingPrinted bool
		extractTxtar       string
		wantInStdout       string
		wantInStderr       string
	}{
		"without directory": {
			args:         []string{},
			wantErr:      cli.ErrInvalidArgs,
			wantInStderr: "Usage: dupfind",
		},
		"version flag": {
			args:         []string{"-version"},
			wantInStderr: "dupfind",
		},
		"nonexistent directory": {
			args:    []string{"nonexistent"},
			wantErr: fs.ErrNotExist,
		},
		"lookup nondup": {
			args:               []string{"[TMPDIR]"},
			extractTxtar:       "testdata/nondup.txtar",
			wantNothingPrinted: true,
		},
		"lookup dup": {
			args:         []string{"[TMPDIR]"},
			extractTxtar: "testdata/dup.txtar",
			wantInStderr: "Duplicate file b.txt of a.txt.\nDuplicate file d.txt of c.txt.",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if tc.extractTxtar != "" {
				tmpDir := t.TempDir()
				for i, arg := range tc.args {
					if arg == "[TMPDIR]" {
						tc.args[i] = tmpDir
					}
				}
				ar, err := txtar.ParseFile(tc.extractTxtar)
				if err != nil {
					t.Fatal(err)
				}
				testutil.ExtractTxtar(t, ar, tmpDir)
			}

			var stdout, stderr bytes.Buffer
			err := run(tc.args, &stdout, &stderr)

			// Don't use && because we want to trap all cases where err is
			// nil.
			if err == nil {
				if tc.wantErr != nil {
					t.Fatalf("must fail with error: %v", tc.wantErr)
				}
			}

			if err != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("got error: %v", err)
			}

			if tc.wantNothingPrinted {
				if stdout.String() != "" {
					t.Errorf("stdout must be empty, got: %q", stdout.String())
				}
				if stderr.String() != "" {
					t.Errorf("stderr must be empty, got: %q", stderr.String())
				}
			}

			if tc.wantInStdout != "" && !strings.Contains(stdout.String(), tc.wantInStdout) {
				t.Errorf("stdout must contain %q, got: %q", tc.wantInStdout, stdout.String())
			}
			if tc.wantInStderr != "" && !strings.Contains(stderr.String(), tc.wantInStderr) {
				t.Errorf("stderr must contain %q, got: %q", tc.wantInStderr, stderr.String())
			}
		})
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()

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

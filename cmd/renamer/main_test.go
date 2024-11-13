// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
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
		args         []string
		wantErr      error
		extractTxtar string
		writeGolden  bool
		wantInStdout string
		wantInStderr string
	}{
		"usage (no directory passed)": {
			args:    []string{},
			wantErr: cli.ErrInvalidArgs,
		},
		"version": {
			args:    []string{"-version"},
			wantErr: cli.ErrExitVersion,
		},
		"rename (existing numbered)": {
			args:         []string{"[TMPDIR]"},
			extractTxtar: "testdata/existing.txtar",
			writeGolden:  true,
		},
		"rename (name sort mode)": {
			args:         []string{"[TMPDIR]"},
			extractTxtar: "testdata/name.txtar",
			writeGolden:  true,
		},
		"rename (size sort mode)": {
			args:         []string{"-sort", "size", "[TMPDIR]"},
			extractTxtar: "testdata/size.txtar",
			writeGolden:  true,
		},
		"rename (type sort mode)": {
			args:         []string{"-sort", "type", "[TMPDIR]"},
			extractTxtar: "testdata/type.txtar",
			writeGolden:  true,
		},
		"rename (unknown sort mode)": {
			args:    []string{"-sort", "foo", "[TMPDIR]"},
			wantErr: errUnknownSortMode,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			for i, arg := range tc.args {
				if arg == "[TMPDIR]" {
					tc.args[i] = tmpDir
				}
			}

			if tc.extractTxtar != "" {
				ar, err := txtar.ParseFile(tc.extractTxtar)
				if err != nil {
					t.Fatal(err)
				}
				testutil.ExtractTxtar(t, ar, tmpDir)
			}

			var stdout, stderr bytes.Buffer
			env := cli.Env{
				Args:   tc.args,
				Stdout: &stdout,
				Stderr: &stderr,
			}
			err := cli.Run(context.Background(), new(app), env)

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

			if tc.wantInStdout != "" && !strings.Contains(stdout.String(), tc.wantInStdout) {
				t.Errorf("stdout must contain %q, got: %q", tc.wantInStdout, stdout.String())
			}
			if tc.wantInStderr != "" && !strings.Contains(stderr.String(), tc.wantInStderr) {
				t.Errorf("stderr must contain %q, got: %q", tc.wantInStderr, stderr.String())
			}

			if tc.extractTxtar != "" && tc.writeGolden {
				golden := strings.TrimSuffix(tc.extractTxtar, filepath.Ext(tc.extractTxtar)) + ".golden"
				got := testutil.BuildTxtar(t, tmpDir)
				if *update {
					if err := os.WriteFile(golden, got, 0o644); err != nil {
						t.Fatalf("unable to write golden file %q: %v", golden, err)
					}
					return
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("unable to read golden file %q: %v", golden, err)
				}
				testutil.AssertEqual(t, got, want)
			}
		})
	}
}

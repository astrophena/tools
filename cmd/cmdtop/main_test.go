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
	"go.astrophena.name/tools/internal/cli"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestRun(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args               []string
		env                map[string]string
		wantErr            error
		wantNothingPrinted bool
		wantInStdout       string
		wantInStderr       string
	}{
		"version": {
			args:         []string{"-version"},
			wantErr:      cli.ErrExitVersion,
			wantInStderr: "cmdtop",
		},
		"invalid number of commands": {
			args:    []string{"foo"},
			wantErr: errInvalidNum,
		},
		"reads from HISTFILE": {
			env: map[string]string{
				"HISTFILE": filepath.Join("testdata", "history"),
			},
			wantInStdout: read(filepath.Join("testdata", "history.golden")),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			getenvFunc := func(env map[string]string) func(string) string {
				return func(name string) string {
					if env == nil {
						return ""
					}
					return env[name]
				}
			}

			var stdout, stderr bytes.Buffer
			env := &cli.Env{
				Args:   tc.args,
				Getenv: getenvFunc(tc.env),
				Stdout: &stdout,
				Stderr: &stderr,
			}
			err := cli.Run(context.Background(), cli.AppFunc(run), env)

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

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"strings"
	"testing"

	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/testutil"
	"go.astrophena.name/tools/internal/txtar"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestRun(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args               []string
		wantErr            error
		wantNothingPrinted bool
		wantInStdout       string
		wantInStderr       string
	}{
		"without directory": {
			args:         []string{},
			wantErr:      cli.ErrArgsNeeded,
			wantInStderr: "Usage: dupfind",
		},
		"version flag": {
			args:         []string{"-version"},
			wantInStderr: "dupfind",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

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

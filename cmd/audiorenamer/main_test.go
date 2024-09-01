// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
)

func TestRun(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args               []string
		wantErr            error
		wantNothingPrinted bool
		wantInStdout       string
		wantInStderr       string
		// Glob of files that should be copied to temporary directory. It does not
		// preserve directory hierarchy.
		copyToDirGlob string
		// List of file names that should be located in temporary directory.
		wantFilesInDir []string
	}{
		"without directory": {
			args:    []string{},
			wantErr: errDirectoryRequired,
		},
		"version": {
			args:         []string{"-version"},
			wantInStderr: "audiorenamer",
		},
		"rename (mp3)": {
			args:          []string{"[TMPDIR]"},
			copyToDirGlob: "testdata/mp3/*.mp3",
			wantFilesInDir: []string{
				"1. Carry You.mp3",
				"2. Colourway.mp3",
				"3. Anchor.mp3",
				"4. Embody Me.mp3",
			},
			wantInStderr: "4 processed: 4 renamed, 0 existing.\n",
		},
		"rename (flac)": {
			args:          []string{"[TMPDIR]"},
			copyToDirGlob: "testdata/flac/*.flac",
			wantFilesInDir: []string{
				"1. Dead Editors.flac",
				"2. Ritual Spirit.flac",
				"3. Voodoo In My Blood.flac",
				"4. Take It There.flac",
			},
			wantInStderr: "4 processed: 4 renamed, 0 existing.\n",
		},
		"skipped not audio": {
			args:          []string{"[TMPDIR]"},
			copyToDirGlob: "testdata/notaudio.mp3",
			wantInStderr:  "0 processed: 0 renamed, 0 existing.\n",
		},
		"dry run": {
			args:          []string{"-dry", "[TMPDIR]"},
			copyToDirGlob: "testdata/mp3/*.mp3",
			wantInStderr:  "Dry run: 4 processed: 4 renamed, 0 existing.",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()

			if tc.copyToDirGlob != "" {
				matches, err := filepath.Glob(tc.copyToDirGlob)
				if err != nil {
					t.Fatal(err)
				}
				for _, match := range matches {
					b, err := os.ReadFile(match)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(tmpDir, filepath.Base(match)), b, 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}

			for i, arg := range tc.args {
				if arg == "[TMPDIR]" {
					tc.args[i] = tmpDir
				}
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

			if tc.wantFilesInDir != nil && len(tc.wantFilesInDir) > 0 {
				gotEntries, err := os.ReadDir(tmpDir)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, e := range gotEntries {
					got = append(got, e.Name())
				}
				slices.Sort(got)
				slices.Sort(tc.wantFilesInDir)
				testutil.AssertEqual(t, got, tc.wantFilesInDir)
			}
		})
	}
}

// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	_ "embed"
	"errors"
	"flag"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/tools/internal/cli"

	"github.com/tobischo/gokeepasslib/v3"
)

var (
	//go:embed testdata/test.kdbx
	db  []byte // password is test
	dbr = func() io.Reader {
		return bytes.NewBuffer(bytes.Clone(db))
	}
	dbPath = filepath.Join("testdata", "test.kdbx")
)

func TestRun(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args         []string
		env          map[string]string
		wantErr      error
		wantInStdout string
		wantInStderr string
	}{
		"prints usage with help flag": {
			args:         []string{"-h"},
			wantErr:      flag.ErrHelp,
			wantInStderr: "Usage: kp",
		},
		"version": {
			args: []string{"-version"},
		},
		"invalid format": {
			args:    []string{"-f", "{{", dbPath, "foo"},
			wantErr: errInvalidFormat,
		},
		"missing entry error": {
			args:    []string{dbPath},
			wantErr: cli.ErrInvalidArgs,
		},
		"missing db": {
			args:    []string{},
			wantErr: cli.ErrInvalidArgs,
		},
		"single entry": {
			args: []string{dbPath, "foo"},
			env: map[string]string{
				"KP_PASSWORD": "test",
			},
			wantInStdout: "bar",
		},
		"custom template": {
			args: []string{"-f", "{{ .GetTitle }}", dbPath, "foo"},
			env: map[string]string{
				"KP_PASSWORD": "test",
			},
			wantInStdout: "foo",
		},
		"list": {
			args: []string{"-l", dbPath},
			env: map[string]string{
				"KP_PASSWORD": "test",
			},
			wantInStdout: "bar\nfoo",
		},
		"custom format for list": {
			args: []string{"-l", "-f", "{{ .GetTitle }}", dbPath},
			env: map[string]string{
				"KP_PASSWORD": "test",
			},
			wantInStdout: "foo\nbar",
		},
	}

	getenvFunc := func(env map[string]string) func(string) string {
		return func(name string) string {
			if env == nil {
				return ""
			}
			return env[name]
		}
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pr, _ := io.Pipe()

			var stdout, stderr bytes.Buffer

			err := run(tc.args, getenvFunc(tc.env), pr, &stdout, &stderr)

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
		})
	}
}

func TestLookup(t *testing.T) {
	cases := map[string]struct {
		r         io.Reader
		entry     string
		password  string
		wantInErr string // or
		wantErr   error
		checkFunc func(t *testing.T, e *gokeepasslib.Entry) // invoked when e is not nil
	}{
		"existing entry": {
			r:        dbr(),
			entry:    "foo",
			password: "test",
			checkFunc: func(t *testing.T, e *gokeepasslib.Entry) {
				testutil.AssertEqual(t, e.GetPassword(), "bar")
			},
		},
		"non-existent entry": {
			r:        dbr(),
			entry:    "foobar",
			password: "test",
			wantErr:  errNotFound,
		},
		"invalid password": {
			r:         dbr(),
			entry:     "foobar",
			password:  "nottest",
			wantInErr: "Wrong password? Database integrity check failed",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			entry, err := lookup(tc.r, tc.password, tc.entry)

			// Don't use && because we want to trap all cases where err is
			// nil.
			if err == nil {
				if tc.wantErr != nil {
					t.Fatalf("must fail with error: %v", tc.wantErr)
				}
			}

			if err != nil && tc.wantInErr != "" && !strings.Contains(err.Error(), tc.wantInErr) {
				t.Fatalf("want to contain %q in error, got %q", tc.wantInErr, err)
			}

			if err != nil && tc.wantInErr == "" && !errors.Is(err, tc.wantErr) {
				t.Fatalf("got error: %v", err)
			}

			if entry != nil && tc.checkFunc != nil {
				tc.checkFunc(t, entry)
			}
		})
	}
}

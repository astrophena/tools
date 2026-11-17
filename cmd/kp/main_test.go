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
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/cli/clitest"
	"go.astrophena.name/base/testutil"

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

	clitest.Run(t, func(t *testing.T) *app {
		return new(app)
	}, map[string]clitest.Case[*app]{
		"prints_usage_with_help_flag": {
			Args:    []string{"-h"},
			WantErr: flag.ErrHelp,
		},
		"version": {
			Args:    []string{"-version"},
			WantErr: cli.ErrExitVersion,
		},
		"nonexistent_file": {
			Args:    []string{"nonexistent.kdbx", "foo"},
			WantErr: fs.ErrNotExist,
		},
		"invalid_format": {
			Args:    []string{"-f", "{{", dbPath, "foo"},
			WantErr: errInvalidFormat,
		},
		"missing_entry_error": {
			Args:    []string{dbPath},
			WantErr: cli.ErrInvalidArgs,
		},
		"missing_db": {
			Args:    []string{},
			WantErr: cli.ErrInvalidArgs,
		},
		"single_entry": {
			Args: []string{dbPath, "foo"},
			Env: map[string]string{
				"KP_PASSWORD": "test",
			},
			WantInStdout: "bar",
		},
		"nonexistent_entry": {
			Args: []string{dbPath, "foobar"},
			Env: map[string]string{
				"KP_PASSWORD": "test",
			},
			WantErr: errNotFound,
		},
		"custom_template": {
			Args: []string{"-f", "{{ .GetTitle }}", dbPath, "foo"},
			Env: map[string]string{
				"KP_PASSWORD": "test",
			},
			WantInStdout: "foo",
		},
		"invalid_field_in_custom_template": {
			Args: []string{"-f", "{{ .Foo }}", dbPath, "foo"},
			Env: map[string]string{
				"KP_PASSWORD": "test",
			},
			WantErrType: template.ExecError{},
		},
		"list": {
			Args: []string{"-l", dbPath},
			Env: map[string]string{
				"KP_PASSWORD": "test",
			},
			WantInStdout: "bar\nfoo",
		},
		"list_invalid_password": {
			Args: []string{"-l", dbPath},
			Env: map[string]string{
				"KP_PASSWORD": "foo",
			},
			WantErr: errFailOpen,
		},
		"custom_format_for_list": {
			Args: []string{"-l", "-f", "{{ .GetTitle }}", dbPath},
			Env: map[string]string{
				"KP_PASSWORD": "test",
			},
			WantInStdout: "foo\nbar",
		},
	})
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
		"existing_entry": {
			r:        dbr(),
			entry:    "foo",
			password: "test",
			checkFunc: func(t *testing.T, e *gokeepasslib.Entry) {
				testutil.AssertEqual(t, e.GetPassword(), "bar")
			},
		},
		"non_existent_entry": {
			r:        dbr(),
			entry:    "foobar",
			password: "test",
			wantErr:  errNotFound,
		},
		"invalid_password": {
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

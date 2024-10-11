package main

import (
	"bytes"
	_ "embed"
	"errors"
	"io"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"

	"github.com/tobischo/gokeepasslib/v3"
)

var (
	//go:embed testdata/test.kdbx
	db  []byte // password is test
	dbr = func() io.Reader {
		return bytes.NewBuffer(bytes.Clone(db))
	}
)

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

// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package clitest provides utilities for testing command-line applications.
package clitest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"go.astrophena.name/tools/internal/cli"
)

// Case represents a single test case for a command-line application.
type Case[App cli.App] struct {
	// Args are the command-line arguments to pass to the application.
	Args []string
	// Stdin is the optional standard input to pass to the application.
	Stdin io.Reader
	// Env are the environment variables to set before running the application.
	Env map[string]string
	// WantErr is the expected error to be returned by the application, checked
	// with errors.Is.
	WantErr error
	// WantErrType is the expected type of the error to be returned by the
	// application, checked with errors.As.
	WantErrType error
	// WantNothingPrinted indicates that no output should be printed to stdout or
	// stderr.
	WantNothingPrinted bool
	// WantInStdout is the expected substring to be present in the stdout output.
	WantInStdout string
	// WantInStderr is the expected substring to be present in the stderr output.
	WantInStderr string
	// CheckFunc is an optional function to perform additional checks after the
	// application has run.
	CheckFunc func(*testing.T, App)
}

// Run runs the provided test cases against the given command-line application.
func Run[App cli.App](t *testing.T, setup func(*testing.T) App, cases map[string]Case[App]) {
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			app := setup(t)

			stdin := tc.Stdin
			if stdin == nil {
				stdin = strings.NewReader("")
			}

			var stdout, stderr bytes.Buffer
			env := &cli.Env{
				Args:   tc.Args,
				Getenv: getenvFunc(tc.Env),
				Stdin:  stdin,
				Stdout: &stdout,
				Stderr: &stderr,
			}

			err := cli.Run(cli.WithEnv(context.Background(), env), app)

			// Don't use && because we want to trap all cases where err is
			// nil.
			if err == nil {
				if tc.WantErr != nil {
					t.Fatalf("must fail with error: %v", tc.WantErr)
				}
				if tc.WantErrType != nil {
					t.Fatalf("must fail with error type %T", tc.WantErrType)
				}
			}

			if err != nil && tc.WantErrType != nil {
				gotErr := reflect.Zero(reflect.TypeOf(tc.WantErrType)).Interface()
				fail := func() {
					t.Fatalf("want error type %T, got %T", tc.WantErrType, err)
				}
				if !errors.As(err, &gotErr) {
					fail()
				}
				if gotErr != nil && reflect.TypeOf(gotErr) != reflect.TypeOf(tc.WantErrType) {
					fail()
				}
			}

			if err != nil && tc.WantErr != nil && !errors.Is(err, tc.WantErr) {
				t.Fatalf("got error: %v", err)
			}

			if tc.WantNothingPrinted {
				if stdout.String() != "" {
					t.Errorf("stdout must be empty, got: %q", stdout.String())
				}
				if stderr.String() != "" {
					t.Errorf("stderr must be empty, got: %q", stderr.String())
				}
			}

			if tc.WantInStdout != "" && !strings.Contains(stdout.String(), tc.WantInStdout) {
				t.Errorf("stdout must contain %q, got: %q", tc.WantInStdout, stdout.String())
			}
			if tc.WantInStderr != "" && !strings.Contains(stderr.String(), tc.WantInStderr) {
				t.Errorf("stderr must contain %q, got: %q", tc.WantInStderr, stderr.String())
			}

			if tc.CheckFunc != nil {
				tc.CheckFunc(t, app)
			}
		})
	}
}

func getenvFunc(env map[string]string) func(string) string {
	return func(name string) string {
		if env == nil {
			return ""
		}
		return env[name]
	}
}

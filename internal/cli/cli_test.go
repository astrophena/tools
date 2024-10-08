// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"go.astrophena.name/tools/internal/version"
)

func TestRun(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err              error
		wantCode         int
		wantErrorPrinted bool
	}{
		"flag.ErrHelp": {
			err:              flag.ErrHelp,
			wantCode:         1,
			wantErrorPrinted: false,
		},
		"ErrInvalidArgs": {
			err:              ErrInvalidArgs,
			wantCode:         1,
			wantErrorPrinted: true,
		},
		"io.EOF": {
			err:              io.EOF,
			wantCode:         1,
			wantErrorPrinted: true,
		},
		"unprintable io.EOF": {
			err:              &unprintableError{io.EOF},
			wantCode:         1,
			wantErrorPrinted: false,
		},
		"nil": {
			err:              nil,
			wantCode:         0,
			wantErrorPrinted: false,
		},
	}

	if tcName := os.Getenv("CLI_TEST_RUN"); tcName != "" {
		Run(func(_ context.Context) error {
			return cases[tcName].err
		})
		return
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var stderr bytes.Buffer

			cmd := exec.Command(os.Args[0], "-test.run=^TestRun$")
			cmd.Env = append(os.Environ(), "CLI_TEST_RUN="+name)
			cmd.Stderr = &stderr
			err := cmd.Run()

			// Check exit code.
			if err == nil {
				if tc.wantCode != 0 {
					t.Fatalf("got exit code 0, wanted %d", tc.wantCode)
				}
			}
			if err != nil {
				execErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("error is not *exec.ExitError, but %T", execErr)
				}
				if execErr.ExitCode() != tc.wantCode {
					t.Fatalf("got exit code %d, wanted %d", execErr.ExitCode(), tc.wantCode)
				}

				// Check error output.
				containsError := strings.Contains(stderr.String(), tc.err.Error())
				if tc.wantErrorPrinted && !containsError {
					t.Fatalf("wanted to have %q in output, got %q", tc.err.Error(), stderr.String())
				}
				if !tc.wantErrorPrinted && containsError {
					t.Fatalf("wanted to didn't have %q in output, got %q", tc.err.Error(), stderr.String())
				}
			}
		})
	}
}

func TestHandleStartup(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		app        *App
		args       []string
		wantOutput string
		wantErr    error
	}{
		"show version": {
			app:        &App{},
			args:       []string{"-version"},
			wantOutput: version.Version().String(),
			wantErr:    ErrExitVersion,
		},
		"no args": {
			app: &App{
				Name:        "testapp",
				Description: "This is a test app.",
			},
			args:       []string{},
			wantOutput: "",
			wantErr:    nil,
		},
		"usage": {
			app: &App{
				Name:        "testapp",
				Description: "This is a test app.",
			},
			args:    []string{"-h"},
			wantErr: flag.ErrHelp,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer

			err := tc.app.HandleStartup(tc.args, &stdout, &stderr)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got error %q, want %q", err, tc.wantErr)
			}

			if tc.wantOutput == "" {
				return
			}

			gotOutput := stderr.String()
			if gotOutput != tc.wantOutput {
				t.Errorf("got output %q, want %q", gotOutput, tc.wantOutput)
			}
		})
	}
}

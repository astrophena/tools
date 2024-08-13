package cli

import (
	"bytes"
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
		"ErrArgsNeeded": {
			err:              ErrArgsNeeded,
			wantCode:         1,
			wantErrorPrinted: false,
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
		Run(cases[tcName].err)
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

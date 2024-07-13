package cli_test

import (
	"bytes"
	"errors"
	"testing"

	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/version"
)

func TestHandleStartup(t *testing.T) {
	cases := map[string]struct {
		app        *cli.App
		args       []string
		wantOutput string
		wantErr    error
	}{
		"show version": {
			app:        &cli.App{},
			args:       []string{"-version"},
			wantOutput: version.Version().String(),
			wantErr:    cli.ErrExitVersion,
		},
		"no args": {
			app: &cli.App{
				Name:        "testapp",
				Description: "This is a test app.",
			},
			args:       []string{},
			wantOutput: "",
			wantErr:    nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			err := tc.app.HandleStartup(tc.args, &stdout, &stderr)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got error %q, want %q", err, tc.wantErr)
			}

			gotOutput := stderr.String()
			if gotOutput != tc.wantOutput {
				t.Errorf("got output %q, want %q", gotOutput, tc.wantOutput)
			}
		})
	}
}

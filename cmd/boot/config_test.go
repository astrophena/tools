// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/base/cli"
)

func TestConfig(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		config      string
		args        []string
		setup       func(t *testing.T, root string, other string)
		wantErr     error
		wantErrText string
		wantOutput  string
	}{
		"default workspace": {
			config:     `boot.configure(workspace = "{root}")`,
			args:       []string{"list"},
			wantOutput: "dotfiles",
		},
		"default entry": {
			config: `boot.configure(workspace = "{root}", entry = "CUSTOM.star")`,
			args:   []string{"list"},
			setup: func(t *testing.T, root string, other string) {
				t.Helper()
				if err := os.Rename(filepath.Join(root, "BOOT.star"), filepath.Join(root, "CUSTOM.star")); err != nil {
					t.Fatal(err)
				}
			},
			wantOutput: "dotfiles",
		},
		"flag workspace overrides config": {
			config:     `boot.configure(workspace = "{other}")`,
			args:       []string{"-C", "{root}", "list"},
			wantOutput: "dotfiles",
		},
		"invalid concurrency": {
			config:      `boot.configure(concurrency = 0)`,
			args:        []string{"list"},
			wantErr:     cli.ErrInvalidArgs,
			wantErrText: "concurrency must be at least 1",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			other := t.TempDir()
			home := t.TempDir()
			writeRecipe(t, root)
			if tc.setup != nil {
				tc.setup(t, root, other)
			}
			config := strings.ReplaceAll(tc.config, "{root}", filepath.ToSlash(root))
			config = strings.ReplaceAll(config, "{other}", filepath.ToSlash(other))
			configDir := filepath.Join(home, ".config", "boot")
			if err := os.MkdirAll(configDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(configDir, "config.star"), []byte(config), 0o644); err != nil {
				t.Fatal(err)
			}
			args := make([]string, len(tc.args))
			for i, arg := range tc.args {
				arg = strings.ReplaceAll(arg, "{root}", root)
				arg = strings.ReplaceAll(arg, "{other}", other)
				args[i] = arg
			}

			var stdout, stderr bytes.Buffer
			env := &cli.Env{
				Args:   args,
				Stdout: &stdout,
				Stderr: &stderr,
				Getenv: func(key string) string {
					if key == "HOME" {
						return home
					}
					if key == "NO_COLOR" {
						return "1"
					}
					return ""
				},
			}
			err := cli.Run(cli.WithEnv(t.Context(), env), new(app))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got error %v, want %v\nstdout:\n%s\nstderr:\n%s", err, tc.wantErr, stdout.String(), stderr.String())
			}
			if tc.wantErrText != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErrText)) {
				t.Fatalf("error does not contain %q: %v", tc.wantErrText, err)
			}
			if tc.wantOutput != "" && !strings.Contains(stdout.String(), tc.wantOutput) {
				t.Fatalf("stdout does not contain %q:\n%s", tc.wantOutput, stdout.String())
			}
		})
	}
}

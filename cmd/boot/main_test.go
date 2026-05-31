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

func TestRun(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args         []string
		setup        func(t *testing.T, root string)
		noRecipe     bool
		wantErr      error
		wantErrText  string
		wantInOutput []string
		wantMissing  []string
		check        func(t *testing.T, root string)
	}{
		"missing command": {
			args:        nil,
			wantErr:     cli.ErrInvalidArgs,
			wantErrText: "usage: boot [flags...] <list|plan|apply>",
		},
		"missing default recipe": {
			args:        []string{"plan"},
			noRecipe:    true,
			wantErr:     cli.ErrInvalidArgs,
			wantErrText: "recipe BOOT.star not found",
		},
		"list": {
			args: []string{"list"},
			wantInOutput: []string{
				"ID",
				"NAME",
				"TAGS",
				"dotfiles",
				"Link dotfiles",
				"filesystem,shell",
				"cache",
				"Create cache",
			},
		},
		"plan does not modify filesystem": {
			args: []string{"plan"},
			wantInOutput: []string{
				"[1/2] Planning task dotfiles: Link dotfiles",
				"change dotfiles: dir ",
				"change dotfiles: symlink ",
				"summary: 2 tasks, 3 actions, 3 would change, 0 skipped, 0 warnings, 0 failed",
			},
			check: func(t *testing.T, root string) {
				t.Helper()
				if _, err := os.Lstat(filepath.Join(root, "home", ".bashrc")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("planned symlink exists: %v", err)
				}
			},
		},
		"apply is idempotent": {
			args: []string{"apply"},
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "home", "local", "data", "bash"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, "bash", "rc"), filepath.Join(root, "home", ".bashrc")); err != nil {
					t.Fatal(err)
				}
			},
			wantInOutput: []string{
				"Report:",
				"Boot ran 2 tasks and checked 3 actions.",
				"It changed 1 action and skipped 2 actions.",
				"No actions failed.",
			},
			wantMissing: []string{"summary:", "change dotfiles:", "skip dotfiles:"},
		},
		"apply verbose prints actions": {
			args: []string{"-verbose", "apply"},
			wantInOutput: []string{
				"[1/2] Applying task dotfiles: Link dotfiles",
				"change dotfiles: dir ",
				"change dotfiles: symlink ",
				"Report:",
			},
			check: func(t *testing.T, root string) {
				t.Helper()
				if _, err := os.Lstat(filepath.Join(root, "home", ".bashrc")); err != nil {
					t.Fatalf("applied symlink missing: %v", err)
				}
			},
		},
		"only filters tasks": {
			args: []string{"-only", "cache", "list"},
			wantInOutput: []string{
				"cache",
				"Create cache",
				"filesystem",
			},
			wantMissing: []string{
				"dotfiles",
				"Link dotfiles",
			},
		},
		"tag filters tasks": {
			args: []string{"-tag", "shell", "list"},
			wantInOutput: []string{
				"dotfiles",
				"Link dotfiles",
				"filesystem,shell",
			},
			wantMissing: []string{
				"cache",
				"Create cache",
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if !tc.noRecipe {
				writeRecipe(t, root)
			}
			if tc.setup != nil {
				tc.setup(t, root)
			}

			var stdout, stderr bytes.Buffer
			env := &cli.Env{
				Args:   append([]string{"-C", root}, tc.args...),
				Stdout: &stdout,
				Stderr: &stderr,
				Getenv: func(key string) string {
					if key == "HOME" {
						return filepath.Join(root, "home")
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
			for _, want := range tc.wantInOutput {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("stdout does not contain %q:\n%s", want, stdout.String())
				}
			}
			for _, missing := range tc.wantMissing {
				if strings.Contains(stdout.String(), missing) {
					t.Errorf("stdout contains %q:\n%s", missing, stdout.String())
				}
			}
			if tc.check != nil {
				tc.check(t, root)
			}
		})
	}
}

func writeRecipe(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "bash"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bash", "rc"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recipe := `# vim: ft=starlark shiftwidth=4

def dotfiles():
    fs.dir("~/local/data/bash")
    fs.symlink("bash/rc", "~/.bashrc")

def cache():
    fs.dir("~/local/cache")

task(
    id="dotfiles",
    name="Link dotfiles",
    tags=["filesystem", "shell"],
    run=dotfiles,
)
task(
    id="cache",
    name="Create cache",
    tags=["filesystem"],
    run=cache,
)
`
	if err := os.WriteFile(filepath.Join(root, "BOOT.star"), []byte(recipe), 0o644); err != nil {
		t.Fatal(err)
	}
}

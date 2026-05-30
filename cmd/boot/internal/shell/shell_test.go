// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package shell

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"
	"go.astrophena.name/tools/cmd/boot/internal/testutil"
	"go.starlark.net/starlark"
)

func TestShellRun(t *testing.T) {
	cases := map[string]struct {
		setup   func(t *testing.T, root string)
		command string
		creates string
		onlyIf  string
		dryRun  bool
		want    boot.Result
		check   func(t *testing.T, root string)
	}{
		"basic run": {
			command: "echo test > {{ROOT}}/test.txt",
			want:    boot.ResultChange,
			check: func(t *testing.T, root string) {
				got, err := os.ReadFile(filepath.Join(root, "test.txt"))
				if err != nil || string(got) != "test\n" {
					t.Errorf("file content = %q, %v; want \"test\\n\", nil", got, err)
				}
			},
		},
		"creates file exists": {
			setup: func(t *testing.T, root string) {
				os.WriteFile(filepath.Join(root, "exists.txt"), []byte("ok"), 0o644)
			},
			command: "echo test > {{ROOT}}/should_not_exist.txt",
			creates: "exists.txt",
			want:    boot.ResultSkip,
			check: func(t *testing.T, root string) {
				if _, err := os.Stat(filepath.Join(root, "should_not_exist.txt")); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("file should not exist")
				}
			},
		},
		"only_if fails": {
			command: "echo test > {{ROOT}}/should_not_exist.txt",
			onlyIf:  "false",
			want:    boot.ResultSkip,
			check: func(t *testing.T, root string) {
				if _, err := os.Stat(filepath.Join(root, "should_not_exist.txt")); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("file should not exist")
				}
			},
		},
		"only_if succeeds": {
			command: "echo test > {{ROOT}}/test.txt",
			onlyIf:  "true",
			want:    boot.ResultChange,
			check: func(t *testing.T, root string) {
				if _, err := os.Stat(filepath.Join(root, "test.txt")); err != nil {
					t.Errorf("file should exist")
				}
			},
		},
		"dry run": {
			command: "echo test > {{ROOT}}/should_not_exist.txt",
			dryRun:  true,
			want:    boot.ResultChange,
			check: func(t *testing.T, root string) {
				if _, err := os.Stat(filepath.Join(root, "should_not_exist.txt")); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("file should not exist in dry run")
				}
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, root)
			}

			rt := &boot.Runtime{Root: root}
			task, thread := testutil.TaskThread("test")

			m := &impl{rt: rt}

			// Replace {{ROOT}} template
			cmd := starlark.String(strings.ReplaceAll(tc.command, "{{ROOT}}", root))

			kwargs := []starlark.Tuple{}
			if tc.creates != "" {
				kwargs = append(kwargs, starlark.Tuple{starlark.String("creates"), starlark.String(tc.creates)})
			}
			if tc.onlyIf != "" {
				kwargs = append(kwargs, starlark.Tuple{starlark.String("only_if"), starlark.String(tc.onlyIf)})
			}

			_, err := m.run(thread, starlark.NewBuiltin("shell.run", m.run), starlark.Tuple{cmd}, kwargs)
			if err != nil {
				t.Fatalf("run failed: %v", err)
			}

			if len(task.Actions) != 1 {
				t.Fatalf("got %d actions, want 1", len(task.Actions))
			}

			res, err := task.Actions[0].Apply(context.Background(), tc.dryRun)
			if err != nil {
				t.Fatalf("apply failed: %v", err)
			}
			if res != tc.want {
				t.Errorf("got result %v, want %v", res, tc.want)
			}

			if tc.check != nil {
				tc.check(t, root)
			}
		})
	}
}

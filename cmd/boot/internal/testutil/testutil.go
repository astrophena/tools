// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package testutil provides helpers for boot module tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.starlark.net/starlark"
)

// TaskThread returns a task and thread associated with each other.
func TaskThread(id string) (*boot.Task, *starlark.Thread) {
	task := &boot.Task{ID: id}
	thread := &starlark.Thread{Name: id}
	boot.SetTask(thread, task)
	return task, thread
}

// WriteCommand writes an executable script into dir.
func WriteCommand(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

// RunGit runs git in dir and fails the test on error.
func RunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	RunGitOutput(t, dir, args...)
}

// RunGitOutput runs git in dir and returns its combined output.
func RunGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package testutil provides helpers for boot module tests.
package testutil

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	boot "go.astrophena.name/tools/cmd/boot/internal"

	"go.starlark.net/starlark"
)

// BuiltinFunc is the implementation signature accepted by starlark.NewBuiltin.
type BuiltinFunc func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error)

// TaskHarness emits actions from module builtins without repeating Starlark
// task setup in every module test.
type TaskHarness struct {
	t      *testing.T
	Task   *boot.Task
	Thread *starlark.Thread
}

// NewTask returns a test task and its associated Starlark thread.
func NewTask(t *testing.T, id string) *TaskHarness {
	t.Helper()
	task := &boot.Task{ID: id}
	thread := &starlark.Thread{Name: id}
	boot.SetTask(thread, task)
	return &TaskHarness{t: t, Task: task, Thread: thread}
}

// EmitOne calls a module builtin and returns its single emitted action.
// Previous actions are cleared so calling EmitOne again models a fresh run.
func (h *TaskHarness) EmitOne(name string, fn BuiltinFunc, args starlark.Tuple, kwargs []starlark.Tuple) boot.Action {
	h.t.Helper()
	h.Task.Actions = nil
	if _, err := fn(h.Thread, starlark.NewBuiltin(name, fn), args, kwargs); err != nil {
		h.t.Fatal(err)
	}
	if len(h.Task.Actions) != 1 {
		h.t.Fatalf("actions = %d, want 1", len(h.Task.Actions))
	}
	return h.Task.Actions[0]
}

func writeCommand(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

// Commands installs executable scripts in a temporary directory and prepends
// it to PATH for the duration of the test.
func Commands(t *testing.T, scripts map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range slices.Sorted(maps.Keys(scripts)) {
		writeCommand(t, dir, name, scripts[name])
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// RunAction executes an action and captures warnings reported through its context.
func RunAction(ctx context.Context, action boot.Action, dryRun bool) (boot.Result, []string, error) {
	var mu sync.Mutex
	var warnings []string
	ctx = boot.WithWarningSink(ctx, func(message string) {
		mu.Lock()
		defer mu.Unlock()
		warnings = append(warnings, message)
	})
	result, err := action.Apply(ctx, dryRun)
	mu.Lock()
	collected := slices.Clone(warnings)
	mu.Unlock()
	return result, collected, err
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

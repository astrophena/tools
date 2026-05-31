// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.starlark.net/starlark"
)

func TestSortTasks(t *testing.T) {
	tasks := []*Task{
		{ID: "C", DependsOn: []string{"A"}},
		{ID: "A"},
		{ID: "B", DependsOn: []string{"A"}},
		{ID: "D", DependsOn: []string{"B", "C"}},
	}

	sorted, err := SortTasks(tasks)
	if err != nil {
		t.Fatal(err)
	}

	var ids []string
	for _, task := range sorted {
		ids = append(ids, task.ID)
	}

	// A must be first. D must be last. C and B can be in any order.
	if ids[0] != "A" {
		t.Errorf("expected A to be first, got %s", ids[0])
	}
	if ids[3] != "D" {
		t.Errorf("expected D to be last, got %s", ids[3])
	}
}

func TestSelectedRejectsUnknownTasks(t *testing.T) {
	engine := &Engine{Tasks: []*Task{{ID: "known"}}}
	_, err := engine.Selected(Selection{Only: []string{"missing"}})
	if err == nil || err.Error() != "unknown task id(s): missing" {
		t.Fatalf("error = %v, want unknown task error", err)
	}
}

func TestSelectedRejectsEmptySelection(t *testing.T) {
	engine := &Engine{Tasks: []*Task{{ID: "known", Tags: []string{"one"}}}}
	_, err := engine.Selected(Selection{Tags: []string{"missing"}})
	if err == nil || err.Error() != "no tasks selected" {
		t.Fatalf("error = %v, want no tasks selected", err)
	}
}

func TestPlanFailFastStopsAfterActionFailure(t *testing.T) {
	var ranSecond bool
	engine := &Engine{Tasks: []*Task{
		{
			ID:   "first",
			Name: "first",
			Run: starlark.NewBuiltin("first", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
				AddAction(thread, Action{
					Summary: "fail",
					Apply: func(context.Context, bool) (Result, error) {
						return "", errors.New("boom")
					},
				})
				return starlark.None, nil
			}),
		},
		{
			ID:   "second",
			Name: "second",
			Run: starlark.NewBuiltin("second", func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
				ranSecond = true
				return starlark.None, nil
			}),
		},
	}}

	var out bytes.Buffer
	err := engine.Run(context.Background(), &out, Selection{}, RunOptions{DryRun: true, FailFast: true})
	if err == nil {
		t.Fatal("Run succeeded, want failure")
	}
	if ranSecond {
		t.Fatal("second task ran after action failure")
	}
}

func TestApplyVerboseFailFastStopsAfterActionFailure(t *testing.T) {
	var ranSecond bool
	engine := &Engine{Tasks: []*Task{
		{
			ID:   "first",
			Name: "first",
			Run: starlark.NewBuiltin("first", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
				AddAction(thread, Action{
					Summary: "fail",
					Apply: func(context.Context, bool) (Result, error) {
						return "", errors.New("boom")
					},
				})
				return starlark.None, nil
			}),
		},
		{
			ID:   "second",
			Name: "second",
			Run: starlark.NewBuiltin("second", func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
				AddAction(thread, Action{
					Summary: "second",
					Apply: func(context.Context, bool) (Result, error) {
						ranSecond = true
						return ResultSkip, nil
					},
				})
				return starlark.None, nil
			}),
		},
	}}

	var out bytes.Buffer
	err := engine.Run(context.Background(), &out, Selection{}, RunOptions{Verbose: true, FailFast: true})
	if err == nil {
		t.Fatal("Run succeeded, want failure")
	}
	if ranSecond {
		t.Fatal("second task actions ran after action failure")
	}
}

func TestApplyFailFastPreservesContinueOnError(t *testing.T) {
	var ranSecond bool
	engine := &Engine{Tasks: []*Task{
		{
			ID:              "first",
			Name:            "first",
			ContinueOnError: true,
			Run: starlark.NewBuiltin("first", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
				AddAction(thread, Action{
					Summary: "fail",
					Apply: func(context.Context, bool) (Result, error) {
						return "", errors.New("boom")
					},
				})
				return starlark.None, nil
			}),
		},
		{
			ID:   "second",
			Name: "second",
			Run: starlark.NewBuiltin("second", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
				ranSecond = true
				AddAction(thread, Action{
					Summary: "ok",
					Apply: func(context.Context, bool) (Result, error) {
						return ResultSkip, nil
					},
				})
				return starlark.None, nil
			}),
		},
	}}

	var out bytes.Buffer
	err := engine.Run(context.Background(), &out, Selection{}, RunOptions{FailFast: true, Concurrency: 1})
	if err == nil {
		t.Fatal("Run succeeded, want failure report")
	}
	if !ranSecond {
		t.Fatal("second task did not run after continuable failure")
	}
	if !strings.Contains(out.String(), "1 action failed") {
		t.Fatalf("output does not report the continuable failure:\n%s", out.String())
	}
}

func TestApplyStopsCurrentTaskOnResultStop(t *testing.T) {
	var ranSecondAction bool
	engine := &Engine{Tasks: []*Task{
		{
			ID:   "first",
			Name: "first",
			Run: starlark.NewBuiltin("first", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
				AddAction(thread, Action{
					Summary: "stop",
					Apply: func(context.Context, bool) (Result, error) {
						return ResultStop, nil
					},
				})
				AddAction(thread, Action{
					Summary: "must not run",
					Apply: func(context.Context, bool) (Result, error) {
						ranSecondAction = true
						return ResultChange, nil
					},
				})
				return starlark.None, nil
			}),
		},
	}}

	var out bytes.Buffer
	err := engine.Run(context.Background(), &out, Selection{}, RunOptions{Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	if ranSecondAction {
		t.Fatal("second action ran after ResultStop")
	}
}

func TestPlanDoesNotStopOnResultStop(t *testing.T) {
	var ranSecondAction bool
	engine := &Engine{Tasks: []*Task{
		{
			ID:   "first",
			Name: "first",
			Run: starlark.NewBuiltin("first", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
				AddAction(thread, Action{
					Summary: "stop",
					Apply: func(context.Context, bool) (Result, error) {
						return ResultStop, nil
					},
				})
				AddAction(thread, Action{
					Summary: "still plan",
					Apply: func(context.Context, bool) (Result, error) {
						ranSecondAction = true
						return ResultSkip, nil
					},
				})
				return starlark.None, nil
			}),
		},
	}}

	var out bytes.Buffer
	err := engine.Run(context.Background(), &out, Selection{}, RunOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !ranSecondAction {
		t.Fatal("plan stopped after ResultStop")
	}
}

func TestTaskRequiresSudo(t *testing.T) {
	engine := &Engine{}
	run := starlark.NewBuiltin("run", func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
		return starlark.None, nil
	})

	_, err := engine.starlarkTask(nil, starlark.NewBuiltin("task", engine.starlarkTask), nil, []starlark.Tuple{
		{starlark.String("id"), starlark.String("root_task")},
		{starlark.String("name"), starlark.String("Root task")},
		{starlark.String("run"), run},
		{starlark.String("requires_sudo"), starlark.True},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !engine.Tasks[0].RequiresSudo {
		t.Fatal("RequiresSudo is false, want true")
	}
}

func TestFailBuiltin(t *testing.T) {
	_, err := fail(nil, starlark.NewBuiltin("fail", fail), starlark.Tuple{starlark.String("unsupported machine")}, nil)
	if err == nil {
		t.Fatal("fail succeeded, want error")
	}
	if err.Error() != "unsupported machine" {
		t.Fatalf("error = %q, want %q", err, "unsupported machine")
	}
}

func TestPrepareSudo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("sudo is not needed when running as root")
	}
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "sudo-ran")
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	engine := &Engine{Runtime: &Runtime{Getenv: func(string) string { return "" }}}
	err := engine.prepareSudo(context.Background(), []*Task{{ID: "root_task", RequiresSudo: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("sudo was not called: %v", err)
	}
}

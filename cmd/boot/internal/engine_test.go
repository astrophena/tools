// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

func TestSelectedRejectsIncompleteDependencies(t *testing.T) {
	engine := &Engine{Tasks: []*Task{
		{ID: "setup", Tags: []string{"base"}},
		{ID: "apps", Tags: []string{"apps"}, DependsOn: []string{"setup"}},
	}}
	_, err := engine.Selected(Selection{Only: []string{"apps"}})
	if err == nil || err.Error() != "selected task dependencies are incomplete: apps depends on unselected task(s): setup" {
		t.Fatalf("error = %v, want incomplete dependency error", err)
	}
}

func TestRunPlanPrintsDynamicActionDescription(t *testing.T) {
	summary := "check packages"
	engine := &Engine{Tasks: []*Task{{
		ID:   "packages",
		Name: "packages",
		Run: starlark.NewBuiltin("packages", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			AddAction(thread, Action{
				Summary: "check packages",
				Describe: func() string {
					return summary
				},
				Apply: func(context.Context, bool) (Result, error) {
					summary = "check packages: would update linux, git"
					return ResultChange, nil
				},
			})
			return starlark.None, nil
		}),
	}}}
	var out bytes.Buffer
	if err := engine.RunPlan(t.Context(), &out, Selection{}, RunOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	assertContains(t, out.String(), "packages: check packages: would update linux, git", "plan output")
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
	err := engine.Run(t.Context(), &out, Selection{}, RunOptions{DryRun: true, FailFast: true})
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
	err := engine.Run(t.Context(), &out, Selection{}, RunOptions{Verbose: true, FailFast: true})
	if err == nil {
		t.Fatal("Run succeeded, want failure")
	}
	if ranSecond {
		t.Fatal("second task actions ran after action failure")
	}
}

func TestApplyFailFastStopsBeforeStartingLaterTasks(t *testing.T) {
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
			Run: starlark.NewBuiltin("second", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
				AddAction(thread, Action{
					Summary: "must not run",
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
	err := engine.Run(t.Context(), &out, Selection{}, RunOptions{FailFast: true, Concurrency: 1})
	if err == nil {
		t.Fatal("Run succeeded, want failure")
	}
	if ranSecond {
		t.Fatal("second task action ran after fail-fast failure")
	}
}

func TestApplySkipsDependentTaskAfterDependencyFailure(t *testing.T) {
	var ranDependent bool
	engine := &Engine{Tasks: []*Task{
		{
			ID:   "setup",
			Name: "setup",
			Run: starlark.NewBuiltin("setup", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
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
			ID:        "apps",
			Name:      "apps",
			DependsOn: []string{"setup"},
			Run: starlark.NewBuiltin("apps", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
				AddAction(thread, Action{
					Summary: "must not run",
					Apply: func(context.Context, bool) (Result, error) {
						ranDependent = true
						return ResultSkip, nil
					},
				})
				return starlark.None, nil
			}),
		},
	}}

	var out bytes.Buffer
	err := engine.Run(t.Context(), &out, Selection{}, RunOptions{Concurrency: 2})
	if err == nil {
		t.Fatal("Run succeeded, want failure")
	}
	if ranDependent {
		t.Fatal("dependent task ran after dependency failure")
	}
	assertContains(t, out.String(), "dependency setup failed", "failure output")
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
	err := engine.Run(t.Context(), &out, Selection{}, RunOptions{FailFast: true, Concurrency: 1})
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
	err := engine.Run(t.Context(), &out, Selection{}, RunOptions{Concurrency: 1})
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
	err := engine.Run(t.Context(), &out, Selection{}, RunOptions{DryRun: true})
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
	var out bytes.Buffer
	err := newSudoPrompter(engine).prepare(t.Context(), &out, []*Task{{ID: "root_task", Name: "Root task", RequiresSudo: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("sudo was not called: %v", err)
	}
	assertContains(t, out.String(), "Boot requests administrator permissions to run the following tasks and actions:", "sudo prompt")
	assertContains(t, out.String(), "  - task root_task: Root task", "sudo prompt")
}

func TestSudoAuthenticateUsesCachedCredentials(t *testing.T) {
	log := filepath.Join(t.TempDir(), "sudo.log")
	writeFakeSudo(t, `#!/bin/sh
printf '%s\n' "$*" >> `+log+`
case "$*" in
"-n -v") exit 0 ;;
*) exit 2 ;;
esac
`)

	prompter := newSudoPrompter(&Engine{Runtime: &Runtime{Interactive: true}})
	var out bytes.Buffer
	if err := prompter.authenticate(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("output = %q, want none", out.String())
	}
	got := strings.TrimSpace(readFile(t, log))
	if got != "-n -v" {
		t.Fatalf("sudo calls = %q, want cached validation only", got)
	}
}

func TestSudoAuthenticateFallsBackInteractively(t *testing.T) {
	log := filepath.Join(t.TempDir(), "sudo.log")
	writeFakeSudo(t, `#!/bin/sh
printf '%s\n' "$*" >> `+log+`
case "$*" in
"-n -v") printf 'sudo: a password is required\n' >&2; exit 1 ;;
"-v") printf '[sudo] password for test:' >&2; exit 0 ;;
*) exit 2 ;;
esac
`)

	prompter := newSudoPrompter(&Engine{Runtime: &Runtime{
		Stdin:       strings.NewReader("password\n"),
		Interactive: true,
	}})
	var out bytes.Buffer
	if err := prompter.authenticate(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	assertContains(t, out.String(), "[sudo] password for test:\n", "sudo prompt output")
	got := strings.TrimSpace(readFile(t, log))
	if got != "-n -v\n-v" {
		t.Fatalf("sudo calls = %q, want cached validation then interactive fallback", got)
	}
}

func TestSudoAuthenticateReportsNonInteractiveFailure(t *testing.T) {
	log := filepath.Join(t.TempDir(), "sudo.log")
	writeFakeSudo(t, `#!/bin/sh
printf '%s\n' "$*" >> `+log+`
case "$*" in
"-n -v") printf 'sudo: a password is required\n' >&2; exit 1 ;;
*) exit 2 ;;
esac
`)

	prompter := newSudoPrompter(&Engine{Runtime: &Runtime{}})
	err := prompter.authenticate(t.Context(), io.Discard)
	if err == nil {
		t.Fatal("sudo authentication succeeded, want failure")
	}
	if !strings.Contains(err.Error(), "sudo: a password is required") {
		t.Fatalf("error = %v, want sudo output", err)
	}
	got := strings.TrimSpace(readFile(t, log))
	if got != "-n -v" {
		t.Fatalf("sudo calls = %q, want non-interactive cached validation only", got)
	}
}

func TestSudoReasonsPreferActionDetails(t *testing.T) {
	reasons := sudoReasons([]*Task{{
		ID:           "setup_etc",
		Name:         "Install /etc files",
		RequiresSudo: true,
		Actions: []Action{{
			Summary:      "sync tree prefs/etc to /etc (sudo)",
			RequiresSudo: true,
		}},
	}})
	if len(reasons) != 1 || reasons[0] != "setup_etc: sync tree prefs/etc to /etc (sudo)" {
		t.Fatalf("sudo reasons = %v, want action reason only", reasons)
	}
}

func TestRunPlanPromptsForSudoOnceBeforeTasks(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("sudo is not needed when running as root")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	engine := &Engine{
		Runtime: &Runtime{Getenv: func(string) string { return "" }},
		Tasks: []*Task{
			{
				ID:   "first",
				Name: "First",
				Run:  sudoAction("first action"),
			},
			{
				ID:   "second",
				Name: "Second",
				Run:  sudoAction("second action"),
			},
		},
	}

	var out bytes.Buffer
	if err := engine.Run(t.Context(), &out, Selection{}, RunOptions{DryRun: true, Verbose: true}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	prompt := "Boot requests administrator permissions to run the following tasks and actions:"
	if strings.Count(got, prompt) != 1 {
		t.Fatalf("sudo prompt count = %d, want 1:\n%s", strings.Count(got, prompt), got)
	}
	if strings.Index(got, prompt) > strings.Index(got, "[1/2] Planning task first") {
		t.Fatalf("sudo prompt was not printed before planning tasks:\n%s", got)
	}
	assertContains(t, got, "  - first: first action", "sudo prompt")
	assertContains(t, got, "  - second: second action", "sudo prompt")
}

func writeFakeSudo(t *testing.T, script string) {
	t.Helper()

	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func sudoAction(summary string) starlark.Callable {
	return starlark.NewBuiltin("sudo", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
		AddAction(thread, Action{
			Summary:      summary,
			RequiresSudo: true,
			Apply: func(context.Context, bool) (Result, error) {
				return ResultSkip, nil
			},
		})
		return starlark.None, nil
	})
}

func TestRunPlanVerbosePrintsSkippedActions(t *testing.T) {
	out := runSkippedActionEngine(t, RunOptions{DryRun: true, Verbose: true})
	assertContains(t, out, "skip noop: already current", "verbose plan output")
}

func TestApplyVerbosePrintsSkippedActions(t *testing.T) {
	out := runSkippedActionEngine(t, RunOptions{Verbose: true})
	assertContains(t, out, "skip noop: already current", "verbose output")
}

func runSkippedActionEngine(t *testing.T, opts RunOptions) string {
	t.Helper()

	engine := &Engine{Tasks: []*Task{{
		ID:   "noop",
		Name: "No-op",
		Run: starlark.NewBuiltin("noop", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			AddAction(thread, Action{
				Summary: "already current",
				Apply: func(context.Context, bool) (Result, error) {
					return ResultSkip, nil
				},
			})
			return starlark.None, nil
		}),
	}}}

	var out bytes.Buffer
	if err := engine.Run(t.Context(), &out, Selection{}, opts); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestApplyProgressOutputSeparatesActionDetails(t *testing.T) {
	var out bytes.Buffer
	rt := &Runtime{Stdout: &out}
	engine := &Engine{
		Runtime: rt,
		Tasks: []*Task{{
			ID:   "packages",
			Name: "Checking packages",
			Run: starlark.NewBuiltin("packages", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
				AddAction(thread, Action{
					Summary: "check packages",
					Apply: func(context.Context, bool) (Result, error) {
						if _, err := Output(rt).Write([]byte("orphaned packages found:\n  - oldlib")); err != nil {
							return "", err
						}
						return ResultWarn, nil
					},
				})
				return starlark.None, nil
			}),
		}},
	}

	if err := engine.Run(t.Context(), &out, Selection{}, RunOptions{Concurrency: 2, Interactive: true}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	assertContains(t, got, "\r\x1b[Korphaned packages found:\n", "progress action detail output")
	assertContains(t, got, "\r\x1b[K  - oldlib\n", "progress action detail output")
}

func assertContains(t *testing.T, got, want, name string) {
	t.Helper()

	if !strings.Contains(got, want) {
		t.Fatalf("%s did not include %q:\n%s", name, want, got)
	}
}

func TestApplyUsesGlobalConcurrentActionLimit(t *testing.T) {
	var running atomic.Int32
	var maxRunning atomic.Int32
	engine := &Engine{Tasks: []*Task{
		{
			ID:   "repos-a",
			Name: "repos a",
			Run:  concurrentTestActions(&running, &maxRunning),
		},
		{
			ID:   "repos-b",
			Name: "repos b",
			Run:  concurrentTestActions(&running, &maxRunning),
		},
	}}

	var out bytes.Buffer
	if err := engine.Run(t.Context(), &out, Selection{}, RunOptions{Concurrency: 2}); err != nil {
		t.Fatal(err)
	}
	if got := maxRunning.Load(); got > 2 {
		t.Fatalf("max concurrent actions = %d, want at most 2", got)
	}
}

func concurrentTestActions(running, maxRunning *atomic.Int32) starlark.Callable {
	return starlark.NewBuiltin("repos", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
		for range 2 {
			AddAction(thread, Action{
				Summary:    "clone",
				Concurrent: true,
				Apply: func(context.Context, bool) (Result, error) {
					current := running.Add(1)
					for {
						max := maxRunning.Load()
						if current <= max || maxRunning.CompareAndSwap(max, current) {
							break
						}
					}
					time.Sleep(25 * time.Millisecond)
					running.Add(-1)
					return ResultSkip, nil
				},
			})
		}
		return starlark.None, nil
	})
}

func TestApplyStopsAfterConcurrentStopResult(t *testing.T) {
	var ranAfterStop atomic.Bool
	engine := &Engine{Tasks: []*Task{{
		ID:   "repos",
		Name: "repos",
		Run: starlark.NewBuiltin("repos", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			AddAction(thread, Action{
				Summary:    "stop",
				Concurrent: true,
				Apply: func(context.Context, bool) (Result, error) {
					return ResultStop, nil
				},
			})
			AddAction(thread, Action{
				Summary:    "skip",
				Concurrent: true,
				Apply: func(context.Context, bool) (Result, error) {
					return ResultSkip, nil
				},
			})
			AddAction(thread, Action{
				Summary: "must not run",
				Apply: func(context.Context, bool) (Result, error) {
					ranAfterStop.Store(true)
					return ResultSkip, nil
				},
			})
			return starlark.None, nil
		}),
	}}}

	var out bytes.Buffer
	if err := engine.Run(t.Context(), &out, Selection{}, RunOptions{Concurrency: 2}); err != nil {
		t.Fatal(err)
	}
	if ranAfterStop.Load() {
		t.Fatal("action after concurrent ResultStop ran")
	}
}

func TestApplyRunsConcurrentActionsInParallel(t *testing.T) {
	var running atomic.Int32
	var overlapped atomic.Bool
	engine := &Engine{Tasks: []*Task{
		{
			ID:   "repos",
			Name: "repos",
			Run: starlark.NewBuiltin("repos", func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
				for range 2 {
					AddAction(thread, Action{
						Summary:    "clone",
						Concurrent: true,
						Apply: func(context.Context, bool) (Result, error) {
							if running.Add(1) == 2 {
								overlapped.Store(true)
							}
							time.Sleep(25 * time.Millisecond)
							running.Add(-1)
							return ResultSkip, nil
						},
					})
				}
				return starlark.None, nil
			}),
		},
	}}

	var out bytes.Buffer
	if err := engine.Run(t.Context(), &out, Selection{}, RunOptions{Concurrency: 2}); err != nil {
		t.Fatal(err)
	}
	if !overlapped.Load() {
		t.Fatal("concurrent actions did not overlap")
	}
}

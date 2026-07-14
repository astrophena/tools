// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"text/tabwriter"

	"go.astrophena.name/base/cli/progressbar"
	"go.astrophena.name/tools/internal/starlark/interpreter"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Engine owns one loaded recipe run.
//
// The engine has two distinct phases:
//
//  1. Load executes the Starlark entrypoint. Top-level Starlark is expected to
//     be declarative: it calls task(...) to register task metadata and does not
//     mutate the host.
//  2. Run evaluates selected task bodies. Task bodies are where modules append
//     Actions to the current task. The engine then plans or applies those
//     actions according to RunOptions.
//
// Keeping task registration separate from action creation lets selection happen
// before any task body is evaluated, and lets plan/apply share the same task
// body evaluation code.
type Engine struct {
	Runtime *Runtime
	Entry   string
	Modules []Module

	Tasks []*Task
}

// Task is a recipe-defined unit of work.
//
// Run is a Starlark callable registered by task(...). It should not do host
// mutation directly; instead it calls module functions such as fs.file or
// pkg.install, and those functions append actions to task.Actions via AddAction.
// Actions is reset every time the task is prepared so repeated
// plan/apply calls do not reuse stale closures or stale host checks.
type Task struct {
	ID              string
	Name            string
	Tags            []string
	DependsOn       []string
	ContinueOnError bool
	RequiresSudo    bool
	Run             starlark.Callable
	Actions         []Action
}

// Action is an idempotent operation emitted by a task.
//
// Apply must implement both check and apply behavior. When dryRun is true it may
// perform read-only probes to decide skip/change, but it must not mutate the
// host. When dryRun is false it should bring the host to the requested state and
// return ResultChange only when it actually changed something.
type Action struct {
	// Summary describes the action in plan, apply, and failure output.
	Summary string
	// Describe returns the current action summary. Actions that discover useful
	// details while probing may use it to refine Summary before the engine prints
	// the result.
	Describe func() string
	Apply    func(context.Context, bool) (Result, error)
	// IsConsent marks an action that must be evaluated during prepare in
	// interactive apply runs. Consent can remove the remaining actions from a task
	// before sudo prompting and before concurrent execution starts.
	IsConsent bool
	// RequiresSudo marks an action as needing sudo even in dry-run mode.
	RequiresSudo bool
	// Concurrent marks an action as eligible to run in parallel with adjacent concurrent actions.
	Concurrent bool
}

func (a Action) description() string {
	if a.Describe != nil {
		if summary := a.Describe(); summary != "" {
			return summary
		}
	}
	return a.Summary
}

// Result describes what an action did.
type Result string

const (
	// ResultSkip means the host already matched the requested state.
	ResultSkip Result = "skip"
	// ResultChange means the action changed the host or would change it in dry-run mode.
	ResultChange Result = "change"
	// ResultStop means no further actions in the current task should run.
	ResultStop Result = "stop"
)

// Selection controls which tasks are listed or run.
type Selection struct {
	Only []string
	Skip []string
	Tags []string
}

// RunOptions controls task execution.
type RunOptions struct {
	DryRun      bool
	FailFast    bool
	Interactive bool
	Concurrency int
	Color       bool
	Verbose     bool
	JSON        bool
}

// Summary describes the outcome of a run.
type Summary struct {
	Tasks    int `json:"tasks"`
	Actions  int `json:"actions"`
	Changed  int `json:"changed"`
	Skipped  int `json:"skipped"`
	Warnings int `json:"warnings"`
	Failed   int `json:"failed"`
}

// Load executes the entrypoint and registers tasks.
//
// Starlark modules are installed as predeclared globals instead of using Python
// import syntax. The interpreter still gets a filesystem loader so recipe files
// can load sibling Starlark files through the repository's interpreter package.
func (e *Engine) Load(ctx context.Context) error {
	predeclared := starlark.StringDict{
		"fail":   starlark.NewBuiltin("fail", fail),
		"host":   starlark.NewBuiltin("host", e.host),
		"task":   starlark.NewBuiltin("task", e.starlarkTask),
		"struct": starlark.NewBuiltin("struct", starlarkstruct.Make),
	}
	for _, mod := range e.Modules {
		predeclared[mod.Name()] = &starlarkstruct.Module{
			Name:    mod.Name(),
			Members: mod.Members(e.Runtime),
		}
	}

	intr := &interpreter.Interpreter{
		Predeclared: predeclared,
		Packages: map[string]interpreter.Loader{
			interpreter.MainPkg: interpreter.FileSystemLoader(e.Runtime.Root),
		},
	}
	if err := intr.Init(ctx); err != nil {
		return err
	}
	_, err := intr.ExecModule(ctx, interpreter.MainPkg, e.Entry)
	if errors.Is(err, interpreter.ErrNoModule) || errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("recipe %s not found in %s", e.Entry, e.Runtime.Root)
	}
	return err
}

func fail(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var message string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "message", &message); err != nil {
		return nil, err
	}
	if message == "" {
		return nil, errors.New("failed")
	}
	return nil, errors.New(message)
}

// List writes selected task metadata.
func (e *Engine) List(w io.Writer, selection Selection) error {
	tasks, err := e.Selected(selection)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tTAGS")
	for _, task := range tasks {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", task.ID, task.Name, strings.Join(task.Tags, ","))
	}
	return tw.Flush()
}

// Run prepares and executes selected tasks.
func (e *Engine) Run(ctx context.Context, w io.Writer, selection Selection, opts RunOptions) error {
	if e.Runtime != nil {
		e.Runtime.Interactive = opts.Interactive
		e.Runtime.Color = opts.Color
	}
	return e.run(ctx, w, selection, opts)
}

// prepare evaluates task bodies and records which tasks are ready to execute.
//
// A task body is run once per engine Run call. It emits Actions into task.Actions
// through module functions. In interactive apply mode, consent actions are
// special-cased here: asking for consent after sudo prompting or after starting
// parallel work would be surprising, so consent runs while the task is still
// being prepared. Accepted consent actions are deleted from the action list so
// they are not counted or executed twice; denied/non-interactive consent marks
// the task as unplanned by returning ResultStop.
func (e *Engine) prepare(ctx context.Context, tasks []*Task, opts RunOptions, warnings *warningCollector) (planned map[string]bool, summary Summary, failures []failure, stopOnError bool) {
	planned = make(map[string]bool)
	summary.Tasks = len(tasks)

	for _, task := range tasks {
		task.Actions = nil
		thread := &starlark.Thread{Name: "boot:" + task.ID}
		SetTask(thread, task)
		if _, err := starlark.Call(thread, task.Run, nil, nil); err != nil {
			if !task.ContinueOnError {
				stopOnError = true
			}
			summary.Failed++
			failures = append(failures, failure{TaskID: task.ID, TaskName: task.Name, Err: err})
			if opts.FailFast && !task.ContinueOnError {
				break
			}
			continue
		}
		if opts.Interactive && !opts.DryRun {
			stop := false
			for i := 0; i < len(task.Actions); i++ {
				action := task.Actions[i]
				if action.IsConsent {
					summary.Actions++
					result, err := warnings.run(ctx, task, action, false)
					if err != nil {
						if !task.ContinueOnError {
							stopOnError = true
						}
						summary.Failed++
						failures = append(failures, failure{TaskID: task.ID, TaskName: task.Name, Action: action.description(), Err: err})
						if opts.FailFast && !task.ContinueOnError {
							stop = true
							break
						}
						continue
					}
					switch result {
					case ResultSkip, ResultStop:
						summary.Skipped++
					case ResultChange:
						summary.Changed++
					}
					if result == ResultStop {
						task.Actions = nil
						stop = true
						break
					}
					task.Actions = slices.Delete(task.Actions, i, i+1)
					i--
				}
			}
			if stop && opts.FailFast && !task.ContinueOnError {
				break
			}
			if stop {
				continue
			}
		}
		planned[task.ID] = true
	}
	return planned, summary, failures, stopOnError
}

// RunPlan plans selected tasks without applying changes.
func (e *Engine) RunPlan(ctx context.Context, w io.Writer, selection Selection, opts RunOptions) error {
	opts.DryRun = true
	return e.Run(ctx, w, selection, opts)
}

func (e *Engine) run(ctx context.Context, w io.Writer, selection Selection, opts RunOptions) error {
	if opts.JSON && e.Runtime != nil {
		oldStdout := e.Runtime.Stdout
		e.Runtime.Stdout = io.Discard
		defer func() { e.Runtime.Stdout = oldStdout }()
	}
	tasks, err := e.Selected(selection)
	if err != nil {
		return err
	}
	warnings := &warningCollector{}
	planned, summary, failures, stopOnError := e.prepare(ctx, tasks, opts, warnings)
	promptOutput := w
	if opts.JSON {
		promptOutput = io.Discard
	}
	if err := newSudoPrompter(e).prepare(ctx, promptOutput, tasks); err != nil {
		return err
	}
	run := execution{planned: planned, summary: summary, failures: failures}
	taskDone := func(*Task) {}
	var pb *progressbar.Bar
	if !opts.DryRun && !opts.Verbose && !opts.JSON && !(stopOnError && opts.FailFast) {
		pb = progressbar.New(w, len(tasks), opts.Interactive)
		pb.Start()
		taskDone = func(task *Task) {
			pb.SetTitle(task.Name)
			pb.Increment()
		}
	}
	if !(stopOnError && opts.FailFast) {
		run = e.execute(ctx, tasks, run, opts, warnings, taskDone)
	}
	if pb != nil {
		pb.Stop(run.summary.Failed > 0)
	}
	collectedWarnings := warnings.all()
	run.summary.Warnings = len(collectedWarnings)
	if opts.JSON {
		err = e.printJSON(w, run, collectedWarnings, opts.DryRun)
	} else if opts.DryRun {
		e.printPlan(w, tasks, run, opts.Verbose)
		e.printWarnings(w, collectedWarnings)
	} else {
		if opts.Verbose {
			e.printVerbose(w, tasks, run)
		}
		e.printReport(w, run.summary, false)
		e.printWarnings(w, collectedWarnings)
		err = e.printFailures(w, run.failures)
	}
	if err != nil {
		return err
	}
	if run.summary.Failed > 0 {
		return errors.New("one or more tasks failed")
	}
	return nil
}

func (e *Engine) printPlan(w io.Writer, tasks []*Task, run execution, verbose bool) {
	actions := run.orderedActions()
	for i, task := range tasks {
		if !run.planned[task.ID] {
			if failure, ok := taskFailure(run.failures, task); ok {
				fmt.Fprintf(w, "[%d/%d] Planning task %s: %s\n", i+1, len(tasks), task.ID, task.Name)
				fmt.Fprintf(w, "%s %s: %v\n", e.color("fail", colorRed), task.ID, failure.Err)
			}
			continue
		}
		fmt.Fprintf(w, "[%d/%d] Planning task %s: %s\n", i+1, len(tasks), task.ID, task.Name)
		for _, action := range actions {
			if action.task != task {
				continue
			}
			if action.err != nil {
				fmt.Fprintf(w, "%s %s: %s: %v\n", e.color("fail", colorRed), task.ID, action.action.description(), action.err)
			} else if action.result == ResultChange {
				fmt.Fprintf(w, "%s %s: %s\n", e.color(string(action.result), colorGreen), task.ID, action.action.description())
			} else if verbose {
				fmt.Fprintf(w, "skip %s: %s\n", task.ID, action.action.description())
			}
		}
	}
	fmt.Fprintf(w, "summary: %d tasks, %d actions, %d would change, %d skipped, %d warnings, %d failed\n",
		run.summary.Tasks, run.summary.Actions, run.summary.Changed, run.summary.Skipped, run.summary.Warnings, run.summary.Failed)
}

func (e *Engine) printVerbose(w io.Writer, tasks []*Task, run execution) {
	actions := run.orderedActions()
	for i, task := range tasks {
		if !run.planned[task.ID] {
			continue
		}
		fmt.Fprintf(w, "[%d/%d] Applying task %s: %s\n", i+1, len(tasks), task.ID, task.Name)
		for _, action := range actions {
			if action.task != task || action.err != nil {
				continue
			}
			label := string(action.result)
			if action.result == ResultChange {
				label = e.color(label, colorGreen)
			}
			fmt.Fprintf(w, "%s %s: %s\n", label, task.ID, action.action.description())
		}
	}
}

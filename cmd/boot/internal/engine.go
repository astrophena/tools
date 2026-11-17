// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package internal runs boot Starlark recipes.
package internal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"text/tabwriter"

	"go.astrophena.name/tools/internal/starlark/interpreter"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Engine loads recipes and runs selected tasks.
type Engine struct {
	Runtime *Runtime
	Entry   string
	Modules []*starlarkstruct.Module

	Tasks []*Task
}

// Task is a recipe-defined unit of work.
type Task struct {
	ID              string
	Name            string
	Tags            []string
	DependsOn       []string
	ContinueOnError bool
	RequiresSudo    bool
	Run             starlark.Callable
}

// Action brings the host toward a task's requested state. Apply may probe the
// host but must not change it when dryRun is true. It returns ResultChange only
// when it changed the host or would do so in dry-run mode.
type Action struct {
	// Title is the short progress label. It defaults to Summary.
	Title string
	// Summary describes the action in plan, verbose, and failure output.
	Summary string
	// Describe can refine Summary after probing the host.
	Describe func() string
	Apply    func(context.Context, bool) (Result, error)
	// IsConsent runs the action during preparation in interactive apply mode.
	IsConsent bool
	// RequiresSudo marks an action as needing sudo during apply.
	RequiresSudo bool
	// Concurrent permits parallel execution with adjacent concurrent actions.
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
	// ResultStop stops the remaining actions in the task.
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

// Load executes the entrypoint and registers tasks. Modules are predeclared
// globals, and recipe files can load sibling Starlark files.
func (e *Engine) Load(ctx context.Context) error {
	predeclared := starlark.StringDict{
		"fail":   starlark.NewBuiltin("fail", fail),
		"host":   starlark.NewBuiltin("host", e.host),
		"task":   starlark.NewBuiltin("task", e.starlarkTask),
		"struct": starlark.NewBuiltin("struct", starlarkstruct.Make),
	}
	for _, mod := range e.Modules {
		predeclared[mod.Name] = mod
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
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.Runtime != nil {
		e.Runtime.Interactive = opts.Interactive
		e.Runtime.Color = opts.Color
		if opts.JSON {
			stdout := e.Runtime.Stdout
			e.Runtime.Stdout = io.Discard
			defer func() { e.Runtime.Stdout = stdout }()
		}
	}

	tasks, err := e.Selected(selection)
	if err != nil {
		return err
	}
	runs := make([]taskRun, len(tasks))
	for i, task := range tasks {
		runs[i].task = task
	}

	warnings := &warningCollector{}
	var progress *runProgress
	if prepare(ctx, runs, opts, warnings) {
		if !opts.DryRun {
			promptOutput := w
			if opts.JSON {
				promptOutput = io.Discard
			}
			if err := e.prepareSudo(ctx, promptOutput, runs); err != nil {
				if canceled := ctx.Err(); canceled != nil {
					return canceled
				}
				return err
			}
		}

		if opts.Interactive && !opts.DryRun && !opts.Verbose && !opts.JSON {
			progress = startProgress(w, len(runs))
		}
		execute(ctx, runs, opts, warnings, progress)
	}

	if err := ctx.Err(); err != nil {
		if progress != nil {
			progress.bar.Stop(false)
		}
		return err
	}

	summary := summarize(runs)
	if progress != nil {
		progress.bar.Stop(summary.Failed > 0)
	}
	collectedWarnings := warnings.all()
	summary.Warnings = len(collectedWarnings)
	if opts.JSON {
		err = printJSON(w, runs, summary, collectedWarnings, opts.DryRun)
	} else {
		if opts.DryRun {
			e.printPlan(w, runs, opts.Verbose)
		} else if opts.Verbose {
			e.printVerbose(w, runs)
		}
		e.printSummary(w, summary, opts.DryRun)
		e.printWarnings(w, collectedWarnings)
		if !opts.DryRun {
			e.printFailures(w, runs, summary.Failed)
		}
	}
	if err != nil {
		return err
	}
	if summary.Failed > 0 {
		return errors.New("one or more tasks failed")
	}
	return nil
}

func prepare(ctx context.Context, runs []taskRun, opts RunOptions, warnings *warningCollector) bool {
	for i := range runs {
		if ctx.Err() != nil {
			return false
		}
		r := &runs[i]
		r.prepare(ctx, opts, warnings)
		if r.status == taskPrepareFailed && opts.FailFast && !r.task.ContinueOnError {
			return false
		}
	}
	return ctx.Err() == nil
}

func (r *taskRun) prepare(ctx context.Context, opts RunOptions, warnings *warningCollector) {
	task := r.task
	var actions []Action
	thread := &starlark.Thread{Name: "boot:" + task.ID}
	CollectActions(thread, &actions)
	stop := context.AfterFunc(ctx, func() { thread.Cancel(ctx.Err().Error()) })
	_, err := starlark.Call(thread, task.Run, nil, nil)
	stop()
	if err != nil {
		r.err = err
		r.status = taskPrepareFailed
		return
	}

	r.status = taskPending
	if !opts.Interactive || opts.DryRun {
		r.prepared = actions
		return
	}

	// Ask for consent before sudo prompting and parallel execution.
	pending := actions[:0]
	for _, action := range actions {
		if ctx.Err() != nil {
			return
		}
		if !action.IsConsent {
			pending = append(pending, action)
			continue
		}
		result := r.runAction(ctx, action, false, warnings)
		r.actions = append(r.actions, result)
		r.consentCount++
		switch {
		case result.err != nil:
			r.status = taskPrepareFailed
			return
		case result.result == ResultStop:
			r.status = taskSkipped
			return
		}
	}
	clear(actions[len(pending):])
	r.prepared = pending
}

func summarize(runs []taskRun) Summary {
	s := Summary{Tasks: len(runs)}
	for i := range runs {
		r := &runs[i]
		if r.err != nil {
			s.Failed++
		}
		s.Actions += len(r.actions)
		for _, action := range r.actions {
			switch {
			case action.err != nil:
				s.Failed++
			case action.result == ResultChange:
				s.Changed++
			case action.result == ResultSkip || action.result == ResultStop:
				s.Skipped++
			}
		}
	}
	return s
}

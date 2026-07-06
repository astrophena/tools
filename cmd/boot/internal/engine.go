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
	"sync"
	"sync/atomic"
	"text/tabwriter"

	"go.astrophena.name/base/cli/progressbar"
	"go.astrophena.name/tools/internal/starlark/interpreter"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"golang.org/x/sync/errgroup"
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
	// ResultWarn means the action found a non-fatal problem.
	ResultWarn Result = "warn"
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

// Run runs selected tasks.
//
// Output mode is chosen here, but all modes share the same preparation step:
// selected task bodies are evaluated into action lists, consent actions may run
// early, and sudo prompts are computed from the resulting actions.
func (e *Engine) Run(ctx context.Context, w io.Writer, selection Selection, opts RunOptions) error {
	if e.Runtime != nil {
		e.Runtime.Interactive = opts.Interactive
		e.Runtime.Color = opts.Color
	}
	if opts.JSON {
		return e.runJSON(ctx, w, selection, opts)
	}
	if opts.DryRun {
		return e.RunPlan(ctx, w, selection, opts)
	}
	if opts.Verbose {
		return e.applyVerbose(ctx, w, selection, opts)
	}
	return e.apply(ctx, w, selection, opts)
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
func (e *Engine) prepare(ctx context.Context, tasks []*Task, opts RunOptions) (planned map[string]bool, summary Summary, failures []failure, stopOnError bool) {
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
					result, err := action.Apply(ctx, false)
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
					case ResultWarn:
						summary.Warnings++
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

// RunPlan plans the selected tasks, evaluating actions without applying changes.
//
// Plan output runs actions sequentially. Planning should be deterministic and
// easy to read; parallelism is reserved for apply where it can reduce real
// wall-clock time.
func (e *Engine) RunPlan(ctx context.Context, w io.Writer, selection Selection, opts RunOptions) error {
	tasks, err := e.Selected(selection)
	if err != nil {
		return err
	}
	if opts.FailFast {
		return e.runPlanSequential(ctx, w, tasks, opts)
	}
	planned, summary, failures, stopOnError := e.prepare(ctx, tasks, opts)
	if err := newSudoPrompter(e).prepare(ctx, w, tasks); err != nil {
		return err
	}

	for i, task := range tasks {
		if !planned[task.ID] {
			if failure, ok := taskFailure(failures, task); ok {
				fmt.Fprintf(w, "[%d/%d] Planning task %s: %s\n", i+1, len(tasks), task.ID, task.Name)
				fmt.Fprintf(w, "%s %s: %v\n", e.color("fail", colorRed), task.ID, failure.Err)
			}
			if stopOnError && opts.FailFast {
				break
			}
			continue
		}
		stop := false
		fmt.Fprintf(w, "[%d/%d] Planning task %s: %s\n", i+1, len(tasks), task.ID, task.Name)
		for _, action := range task.Actions {
			summary.Actions++
			result, err := action.Apply(ctx, true)
			if err != nil {
				summary.Failed++
				fmt.Fprintf(w, "%s %s: %s: %v\n", e.color("fail", colorRed), task.ID, action.description(), err)
				if opts.FailFast && !task.ContinueOnError {
					stop = true
					break
				}
				continue
			}
			switch result {
			case ResultChange:
				summary.Changed++
				fmt.Fprintf(w, "%s %s: %s\n", e.color(string(result), colorGreen), task.ID, action.description())
			case ResultSkip:
				summary.Skipped++
				if opts.Verbose {
					fmt.Fprintf(w, "skip %s: %s\n", task.ID, action.description())
				}
			case ResultWarn:
				summary.Warnings++
				fmt.Fprintf(w, "%s %s: %s\n", e.color(string(result), colorRed), task.ID, action.description())
			case ResultStop:
				summary.Skipped++
				if opts.Verbose {
					fmt.Fprintf(w, "skip %s: %s\n", task.ID, action.description())
				}
			}
		}
		if stop {
			break
		}
	}

	fmt.Fprintf(
		w,
		"summary: %d tasks, %d actions, %d would change, %d skipped, %d warnings, %d failed\n",
		summary.Tasks,
		summary.Actions,
		summary.Changed,
		summary.Skipped,
		summary.Warnings,
		summary.Failed,
	)
	if summary.Failed > 0 {
		return errors.New("one or more tasks failed")
	}
	return nil
}

func (e *Engine) runPlanSequential(ctx context.Context, w io.Writer, tasks []*Task, opts RunOptions) error {
	summary := Summary{Tasks: len(tasks)}
	sudo := newSudoPrompter(e)

	for i, task := range tasks {
		stop := false
		fmt.Fprintf(w, "[%d/%d] Planning task %s: %s\n", i+1, len(tasks), task.ID, task.Name)
		task.Actions = nil
		thread := &starlark.Thread{Name: "boot:" + task.ID}
		SetTask(thread, task)
		if _, err := starlark.Call(thread, task.Run, nil, nil); err != nil {
			summary.Failed++
			fmt.Fprintf(w, "%s %s: %v\n", e.color("fail", colorRed), task.ID, err)
			if !task.ContinueOnError {
				break
			}
			continue
		}
		if err := sudo.prepare(ctx, w, []*Task{task}); err != nil {
			return err
		}
		for _, action := range task.Actions {
			summary.Actions++
			result, err := action.Apply(ctx, true)
			if err != nil {
				summary.Failed++
				fmt.Fprintf(w, "%s %s: %s: %v\n", e.color("fail", colorRed), task.ID, action.description(), err)
				if !task.ContinueOnError {
					stop = true
					break
				}
				continue
			}
			switch result {
			case ResultChange:
				summary.Changed++
				fmt.Fprintf(w, "%s %s: %s\n", e.color(string(result), colorGreen), task.ID, action.description())
			case ResultSkip:
				summary.Skipped++
				if opts.Verbose {
					fmt.Fprintf(w, "skip %s: %s\n", task.ID, action.description())
				}
			case ResultWarn:
				summary.Warnings++
				fmt.Fprintf(w, "%s %s: %s\n", e.color(string(result), colorRed), task.ID, action.description())
			case ResultStop:
				summary.Skipped++
				if opts.Verbose {
					fmt.Fprintf(w, "skip %s: %s\n", task.ID, action.description())
				}
			}
		}
		if stop {
			break
		}
	}

	fmt.Fprintf(
		w,
		"summary: %d tasks, %d actions, %d would change, %d skipped, %d warnings, %d failed\n",
		summary.Tasks,
		summary.Actions,
		summary.Changed,
		summary.Skipped,
		summary.Warnings,
		summary.Failed,
	)
	if summary.Failed > 0 {
		return errors.New("one or more tasks failed")
	}
	return nil
}

// apply runs prepared actions, using parallelism only when requested.
//
// For -j 1 this delegates to the sequential path so fail-fast and dependency
// behavior is deterministic. For -j N, each task waits for its declared
// dependencies to finish, then competes for a task slot. Concurrent action runs
// also share the same global limit; this keeps a recipe from multiplying
// parallelism by running N tasks each with N concurrent actions.
func (e *Engine) apply(ctx context.Context, w io.Writer, selection Selection, opts RunOptions) error {
	tasks, err := e.Selected(selection)
	if err != nil {
		return err
	}

	planned, summary, failures, stopOnError := e.prepare(ctx, tasks, opts)
	if err := newSudoPrompter(e).prepare(ctx, w, tasks); err != nil {
		return err
	}
	hadFailures := len(failures) > 0

	if stopOnError && opts.FailFast {
		e.printReport(w, summary, false)
		e.printFailures(w, failures)
		return errors.New("one or more tasks failed")
	}

	concurrency := max(opts.Concurrency, 1)
	if concurrency == 1 {
		return e.applySequentialPrepared(ctx, w, tasks, planned, summary, failures, opts, false)
	}

	pb := progressbar.New(w, len(tasks), opts.Interactive)
	pb.Start()

	// doneCh records completion of every selected task. status records whether a
	// completed dependency succeeded. Both maps include unplanned tasks so
	// dependents can distinguish "dependency skipped during preparation" from
	// "dependency failed during preparation".
	doneCh := make(map[string]chan struct{})
	status := make(map[string]taskStatus)
	for _, task := range tasks {
		doneCh[task.ID] = make(chan struct{})
		status[task.ID] = taskPending
	}
	for _, task := range tasks {
		if planned[task.ID] {
			continue
		}
		if _, ok := taskFailure(failures, task); ok {
			status[task.ID] = taskFailed
		} else {
			status[task.ID] = taskSucceeded
		}
	}

	g, ctx := errgroup.WithContext(ctx)
	// Two limiters are deliberate: task concurrency bounds how many independent
	// task streams can be active, while action concurrency bounds the number of
	// expensive operations inside those streams. Using the same numeric limit for
	// both keeps the CLI simple while preserving a global cap on concurrent host
	// mutations.
	tasksLimiter := newActionLimiter(concurrency)
	actions := newActionLimiter(concurrency)

	var mu sync.Mutex

	for _, task := range tasks {
		g.Go(func() error {
			// Each goroutine owns one task's action stream. Shared summary/failure state
			// is protected by mu; dependency completion is signaled by closing doneCh.
			setStatus := func(next taskStatus) {
				mu.Lock()
				status[task.ID] = next
				mu.Unlock()
			}
			defer close(doneCh[task.ID])

			for _, dep := range task.DependsOn {
				if ch, ok := doneCh[dep]; ok {
					select {
					case <-ch:
					case <-ctx.Done():
						return ctx.Err()
					}
					mu.Lock()
					depFailed := status[dep] == taskFailed
					mu.Unlock()
					if depFailed {
						err := fmt.Errorf("dependency %s failed", dep)
						mu.Lock()
						hadFailures = true
						if !task.ContinueOnError {
							stopOnError = true
						}
						summary.Failed++
						failures = append(failures, failure{TaskID: task.ID, TaskName: task.Name, Err: err})
						status[task.ID] = taskFailed
						mu.Unlock()
						pb.Increment()
						if opts.FailFast && !task.ContinueOnError {
							return err
						}
						return nil
					}
				}
			}

			if err := tasksLimiter.acquire(ctx); err != nil {
				return err
			}
			defer tasksLimiter.release()

			mu.Lock()
			if !planned[task.ID] || (stopOnError && opts.FailFast) {
				mu.Unlock()
				pb.Increment()
				return nil
			}
			mu.Unlock()

			pb.SetTitle(task.Name)
			taskHadFailure := false

			applyAction := func(action Action) (Result, error) {
				mu.Lock()
				summary.Actions++
				mu.Unlock()

				var (
					result Result
					err    error
				)
				if err = actions.acquire(ctx); err == nil {
					result, err = action.Apply(ctx, opts.DryRun)
					actions.release()
				}
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					taskHadFailure = true
					hadFailures = true
					if !task.ContinueOnError {
						stopOnError = true
					}
					summary.Failed++
					failures = append(failures, failure{
						TaskID:   task.ID,
						TaskName: task.Name,
						Action:   action.description(),
						Err:      err,
					})
					return result, err
				}
				switch result {
				case ResultChange:
					summary.Changed++
				case ResultSkip:
					summary.Skipped++
				case ResultWarn:
					summary.Warnings++
				case ResultStop:
					summary.Skipped++
				}
				return result, nil
			}

			for i := 0; i < len(task.Actions); {
				action := task.Actions[i]
				if !action.Concurrent || concurrency == 1 {
					result, err := applyAction(action)
					if err != nil && opts.FailFast && !task.ContinueOnError {
						pb.Increment()
						return err
					}
					if result == ResultStop {
						break
					}
					i++
					continue
				}

				// Adjacent Concurrent actions form a batch. Non-concurrent actions act as
				// ordering barriers, which is useful for recipes such as "sync files, then
				// restart service".
				end := i + 1
				for end < len(task.Actions) && task.Actions[end].Concurrent {
					end++
				}
				batch, batchCtx := errgroup.WithContext(ctx)
				var shouldStop atomic.Bool
				for _, action := range task.Actions[i:end] {
					batch.Go(func() error {
						result, err := applyAction(action)
						if result == ResultStop {
							shouldStop.Store(true)
						}
						if err != nil && opts.FailFast && !task.ContinueOnError {
							return err
						}
						select {
						case <-batchCtx.Done():
							return batchCtx.Err()
						default:
							return nil
						}
					})
				}
				if err := batch.Wait(); err != nil {
					pb.Increment()
					return err
				}
				if shouldStop.Load() {
					break
				}
				i = end
			}

			if taskHadFailure {
				setStatus(taskFailed)
			} else {
				setStatus(taskSucceeded)
			}
			pb.Increment()
			return nil
		})
	}

	err = g.Wait()
	pb.Stop(hadFailures || err != nil)

	if err != nil && !hadFailures {
		failures = append(failures, failure{Err: err})
	}
	e.printReport(w, summary, false)
	if err := e.printFailures(w, failures); err != nil {
		return err
	}
	if err != nil {
		return err
	}
	if hadFailures {
		return errors.New("one or more tasks failed")
	}
	return nil
}

func (e *Engine) applyVerbose(ctx context.Context, w io.Writer, selection Selection, opts RunOptions) error {
	tasks, err := e.Selected(selection)
	if err != nil {
		return err
	}

	planned, summary, failures, stopOnError := e.prepare(ctx, tasks, opts)
	if err := newSudoPrompter(e).prepare(ctx, w, tasks); err != nil {
		return err
	}
	if stopOnError && opts.FailFast {
		e.printReport(w, summary, false)
		e.printFailures(w, failures)
		return errors.New("one or more tasks failed")
	}
	return e.applySequentialPrepared(ctx, w, tasks, planned, summary, failures, opts, true)
}

// applySequentialPrepared is used for verbose apply and for non-verbose -j 1.
// It shares dependency failure handling with the parallel path but avoids the
// progress bar and goroutine scheduling, making the single-threaded behavior
// predictable and easier to debug.
func (e *Engine) applySequentialPrepared(ctx context.Context, w io.Writer, tasks []*Task, planned map[string]bool, summary Summary, failures []failure, opts RunOptions, verbose bool) error {
	status := initialTaskStatus(tasks, planned, failures)
	for i, task := range tasks {
		if !planned[task.ID] {
			continue
		}
		if dep, failed := failedDependency(status, task); failed {
			err := fmt.Errorf("dependency %s failed", dep)
			summary.Failed++
			failures = append(failures, failure{TaskID: task.ID, TaskName: task.Name, Err: err})
			status[task.ID] = taskFailed
			if opts.FailFast && !task.ContinueOnError {
				break
			}
			continue
		}
		status[task.ID] = taskSucceeded
		stop := false
		if verbose {
			fmt.Fprintf(w, "[%d/%d] Applying task %s: %s\n", i+1, len(tasks), task.ID, task.Name)
		}
		for _, action := range task.Actions {
			summary.Actions++
			result, err := action.Apply(ctx, false)
			if err != nil {
				status[task.ID] = taskFailed
				summary.Failed++
				failures = append(failures, failure{
					TaskID:   task.ID,
					TaskName: task.Name,
					Action:   action.description(),
					Err:      err,
				})
				if opts.FailFast && !task.ContinueOnError {
					stop = true
					break
				}
				continue
			}
			switch result {
			case ResultChange:
				summary.Changed++
				if verbose {
					fmt.Fprintf(w, "%s %s: %s\n", e.color(string(result), colorGreen), task.ID, action.description())
				}
			case ResultSkip:
				summary.Skipped++
				if verbose {
					fmt.Fprintf(w, "skip %s: %s\n", task.ID, action.description())
				}
			case ResultWarn:
				summary.Warnings++
				if verbose {
					fmt.Fprintf(w, "%s %s: %s\n", e.color(string(result), colorRed), task.ID, action.description())
				}
			case ResultStop:
				summary.Skipped++
				if verbose {
					fmt.Fprintf(w, "skip %s: %s\n", task.ID, action.description())
				}
				stop = true
			}
			if stop {
				break
			}
		}
		if stop {
			break
		}
	}

	e.printReport(w, summary, false)
	if err := e.printFailures(w, failures); err != nil {
		return err
	}
	if summary.Failed > 0 {
		return errors.New("one or more tasks failed")
	}
	return nil
}

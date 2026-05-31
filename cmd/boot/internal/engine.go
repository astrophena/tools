// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"

	"go.astrophena.name/base/cli/progressbar"
	"go.astrophena.name/tools/internal/starlark/interpreter"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"golang.org/x/sync/errgroup"
)

// Engine loads recipes, registers tasks, and runs selected tasks.
type Engine struct {
	Runtime *Runtime
	Entry   string
	Modules []Module

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
	Actions         []Action
}

// Action is an idempotent operation emitted by a task.
type Action struct {
	Summary string
	Apply   func(context.Context, bool) (Result, error)
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
func (e *Engine) Load(ctx context.Context) error {
	predeclared := starlark.StringDict{
		"fail":   starlark.NewBuiltin("fail", fail),
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
func (e *Engine) Run(ctx context.Context, w io.Writer, selection Selection, opts RunOptions) error {
	if e.Runtime != nil {
		e.Runtime.Interactive = opts.Interactive
	}
	if opts.JSON {
		return e.runJSON(ctx, w, selection, opts)
	}
	if opts.DryRun {
		return e.plan(ctx, w, selection, opts)
	}
	if opts.Verbose {
		return e.applyVerbose(ctx, w, selection, opts)
	}
	return e.apply(ctx, w, selection, opts)
}

func (e *Engine) plan(ctx context.Context, w io.Writer, selection Selection, opts RunOptions) error {
	tasks, err := e.Selected(selection)
	if err != nil {
		return err
	}
	summary := Summary{Tasks: len(tasks)}

	for i, task := range tasks {
		stop := false
		fmt.Fprintf(w, "[%d/%d] Planning task %s: %s\n", i+1, len(tasks), task.ID, task.Name)
		task.Actions = nil
		thread := &starlark.Thread{Name: "boot:" + task.ID}
		SetTask(thread, task)
		if _, err := starlark.Call(thread, task.Run, nil, nil); err != nil {
			summary.Failed++
			fmt.Fprintf(w, "%s %s: %v\n", e.color("fail", colorRed), task.ID, err)
			if opts.FailFast && !task.ContinueOnError {
				break
			}
			continue
		}
		for _, action := range task.Actions {
			summary.Actions++
			result, err := action.Apply(ctx, true)
			if err != nil {
				summary.Failed++
				fmt.Fprintf(w, "%s %s: %s: %v\n", e.color("fail", colorRed), task.ID, action.Summary, err)
				if opts.FailFast && !task.ContinueOnError {
					stop = true
					break
				}
				continue
			}
			switch result {
			case ResultChange:
				summary.Changed++
				fmt.Fprintf(w, "%s %s: %s\n", e.color(string(result), colorGreen), task.ID, action.Summary)
			case ResultSkip:
				summary.Skipped++
			case ResultWarn:
				summary.Warnings++
				fmt.Fprintf(w, "%s %s: %s\n", e.color(string(result), colorRed), task.ID, action.Summary)
			case ResultStop:
				summary.Skipped++
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

func (e *Engine) apply(ctx context.Context, w io.Writer, selection Selection, opts RunOptions) error {
	tasks, err := e.Selected(selection)
	if err != nil {
		return err
	}
	if err := e.prepareSudo(ctx, tasks); err != nil {
		return err
	}
	summary := Summary{
		Tasks: len(tasks),
	}

	concurrency := max(opts.Concurrency, 1)

	pb := progressbar.New(w, len(tasks), opts.Interactive)
	pb.Start()

	var (
		mu          sync.Mutex
		hadFailures bool
		stopOnError bool
		failures    []failure
	)

	doneCh := make(map[string]chan struct{})
	for _, task := range tasks {
		doneCh[task.ID] = make(chan struct{})
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for _, task := range tasks {
		g.Go(func() error {
			defer close(doneCh[task.ID])

			for _, dep := range task.DependsOn {
				if ch, ok := doneCh[dep]; ok {
					select {
					case <-ch:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}

			mu.Lock()
			if stopOnError && opts.FailFast {
				mu.Unlock()
				return nil
			}
			mu.Unlock()

			task.Actions = nil

			pb.SetTitle(task.Name)
			thread := &starlark.Thread{Name: "boot:" + task.ID}
			SetTask(thread, task)
			_, err := starlark.Call(thread, task.Run, nil, nil)
			if err != nil {
				mu.Lock()
				hadFailures = true
				if !task.ContinueOnError {
					stopOnError = true
				}
				summary.Failed++
				failures = append(failures, failure{TaskID: task.ID, TaskName: task.Name, Err: err})
				mu.Unlock()

				if opts.FailFast && !task.ContinueOnError {
					pb.Increment()
					return err
				}
				pb.Increment()
				return nil
			}

			for _, action := range task.Actions {
				mu.Lock()
				summary.Actions++
				mu.Unlock()

				result, err := action.Apply(ctx, opts.DryRun)
				if err != nil {
					mu.Lock()
					hadFailures = true
					if !task.ContinueOnError {
						stopOnError = true
					}
					summary.Failed++
					failures = append(failures, failure{
						TaskID:   task.ID,
						TaskName: task.Name,
						Action:   action.Summary,
						Err:      err,
					})
					mu.Unlock()

					if opts.FailFast && !task.ContinueOnError {
						pb.Increment()
						return err
					}
					continue
				}

				mu.Lock()
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
				mu.Unlock()
				if result == ResultStop {
					break
				}
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
	if err := e.prepareSudo(ctx, tasks); err != nil {
		return err
	}
	summary := Summary{Tasks: len(tasks)}
	var failures []failure

	for i, task := range tasks {
		stop := false
		fmt.Fprintf(w, "[%d/%d] Applying task %s: %s\n", i+1, len(tasks), task.ID, task.Name)
		task.Actions = nil
		thread := &starlark.Thread{Name: "boot:" + task.ID}
		SetTask(thread, task)
		if _, err := starlark.Call(thread, task.Run, nil, nil); err != nil {
			summary.Failed++
			failures = append(failures, failure{TaskID: task.ID, TaskName: task.Name, Err: err})
			if opts.FailFast && !task.ContinueOnError {
				break
			}
			continue
		}
		for _, action := range task.Actions {
			summary.Actions++
			result, err := action.Apply(ctx, false)
			if err != nil {
				summary.Failed++
				failures = append(failures, failure{
					TaskID:   task.ID,
					TaskName: task.Name,
					Action:   action.Summary,
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
				fmt.Fprintf(w, "%s %s: %s\n", e.color(string(result), colorGreen), task.ID, action.Summary)
			case ResultSkip:
				summary.Skipped++
				fmt.Fprintf(w, "skip %s: %s\n", task.ID, action.Summary)
			case ResultWarn:
				summary.Warnings++
				fmt.Fprintf(w, "%s %s: %s\n", e.color(string(result), colorRed), task.ID, action.Summary)
			case ResultStop:
				summary.Skipped++
				fmt.Fprintf(w, "skip %s: %s\n", task.ID, action.Summary)
				stop = true
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

type failure struct {
	TaskID   string
	TaskName string
	Action   string
	Err      error
}

func (e *Engine) printReport(w io.Writer, summary Summary, dryRun bool) {
	changeText := "changed"
	if dryRun {
		changeText = "would change"
	}
	fmt.Fprintf(w, "%s\n", e.color("Report:", colorBold))
	fmt.Fprintf(w, "  Boot ran %d %s and checked %d %s.\n",
		summary.Tasks,
		plural(summary.Tasks, "task", "tasks"),
		summary.Actions,
		plural(summary.Actions, "action", "actions"),
	)
	fmt.Fprintf(w, "  It %s %s and skipped %s.\n",
		changeText,
		e.color(fmt.Sprintf("%d %s", summary.Changed, plural(summary.Changed, "action", "actions")), colorGreen),
		fmt.Sprintf("%d %s", summary.Skipped, plural(summary.Skipped, "action", "actions")),
	)
	if summary.Warnings > 0 {
		fmt.Fprintf(w, "  It reported %s.\n", e.color(fmt.Sprintf("%d %s", summary.Warnings, plural(summary.Warnings, "warning", "warnings")), colorRed))
	}
	if summary.Failed == 0 {
		fmt.Fprintf(w, "  %s\n", e.color("No actions failed.", colorGreen))
		return
	}
	fmt.Fprintf(w, "  %s\n", e.color(fmt.Sprintf("%d %s failed.", summary.Failed, plural(summary.Failed, "action", "actions")), colorRed))
}

func (e *Engine) printFailures(w io.Writer, failures []failure) error {
	if len(failures) == 0 {
		return nil
	}
	fmt.Fprintf(w, "%s\n", e.color("errors:", colorRed))
	for _, failure := range failures {
		logPath, err := writeFailureLog(failure)
		if err != nil {
			return err
		}
		if failure.TaskID != "" {
			fmt.Fprintf(w, "%s\n", e.color(fmt.Sprintf("  task: %s (%s)", failure.TaskID, failure.TaskName), colorRed))
		}
		if failure.Action != "" {
			fmt.Fprintf(w, "%s\n", e.color("    action: "+failure.Action, colorRed))
		}
		fmt.Fprintf(w, "%s\n", e.color("    error: "+firstLine(failure.Err.Error()), colorRed))
		fmt.Fprintf(w, "%s\n", e.color("    log: "+logPath, colorRed))
		if output := restLines(failure.Err.Error()); output != "" {
			fmt.Fprintf(w, "%s\n", e.color("    output:", colorRed))
			for line := range strings.SplitSeq(output, "\n") {
				fmt.Fprintf(w, "%s\n", e.color("      "+line, colorRed))
			}
		}
	}
	return nil
}

func writeFailureLog(f failure) (string, error) {
	file, err := os.CreateTemp("", "boot-error-*.log")
	if err != nil {
		return "", err
	}
	defer file.Close()
	if f.TaskID != "" {
		fmt.Fprintf(file, "task: %s (%s)\n", f.TaskID, f.TaskName)
	}
	if f.Action != "" {
		fmt.Fprintf(file, "action: %s\n", f.Action)
	}
	fmt.Fprintf(file, "error:\n%s\n", f.Err)
	return file.Name(), nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func restLines(s string) string {
	_, rest, ok := strings.Cut(strings.TrimSpace(s), "\n")
	if !ok {
		return ""
	}
	return strings.TrimSpace(rest)
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

type jsonRun struct {
	DryRun  bool         `json:"dry_run"`
	Summary Summary      `json:"summary"`
	Actions []jsonAction `json:"actions"`
}

type jsonAction struct {
	TaskID   string `json:"task_id"`
	TaskName string `json:"task_name"`
	Summary  string `json:"summary,omitempty"`
	Result   Result `json:"result,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (e *Engine) runJSON(ctx context.Context, w io.Writer, selection Selection, opts RunOptions) error {
	if e.Runtime != nil {
		oldStdout := e.Runtime.Stdout
		e.Runtime.Stdout = io.Discard
		defer func() { e.Runtime.Stdout = oldStdout }()
	}
	tasks, err := e.Selected(selection)
	if err != nil {
		return err
	}
	if !opts.DryRun {
		if err := e.prepareSudo(ctx, tasks); err != nil {
			return err
		}
	}

	report := jsonRun{DryRun: opts.DryRun, Summary: Summary{Tasks: len(tasks)}}
	for _, task := range tasks {
		task.Actions = nil
		thread := &starlark.Thread{Name: "boot:" + task.ID}
		SetTask(thread, task)
		if _, err := starlark.Call(thread, task.Run, nil, nil); err != nil {
			report.Summary.Failed++
			report.Actions = append(report.Actions, jsonAction{TaskID: task.ID, TaskName: task.Name, Error: err.Error()})
			if opts.FailFast && !task.ContinueOnError {
				break
			}
			continue
		}
		stop := false
		for _, action := range task.Actions {
			report.Summary.Actions++
			result, err := action.Apply(ctx, opts.DryRun)
			item := jsonAction{TaskID: task.ID, TaskName: task.Name, Summary: action.Summary, Result: result}
			if err != nil {
				report.Summary.Failed++
				item.Error = err.Error()
				report.Actions = append(report.Actions, item)
				if opts.FailFast && !task.ContinueOnError {
					stop = true
					break
				}
				continue
			}
			switch result {
			case ResultChange:
				report.Summary.Changed++
			case ResultSkip, ResultStop:
				report.Summary.Skipped++
			case ResultWarn:
				report.Summary.Warnings++
			}
			report.Actions = append(report.Actions, item)
			if result == ResultStop && !opts.DryRun {
				break
			}
		}
		if stop {
			break
		}
	}
	if err := json.NewEncoder(w).Encode(report); err != nil {
		return err
	}
	if report.Summary.Failed > 0 {
		return errors.New("one or more tasks failed")
	}
	return nil
}

type colorCode string

const (
	colorBold  colorCode = "\033[1m"
	colorGreen colorCode = "\033[32m"
	colorRed   colorCode = "\033[31m"
)

func (e *Engine) color(s string, code colorCode) string {
	if !e.colorEnabled() {
		return s
	}
	return string(code) + s + "\033[0m"
}

func (e *Engine) colorEnabled() bool {
	return e.Runtime == nil || e.Runtime.Getenv == nil || e.Runtime.Getenv("NO_COLOR") == ""
}

func (e *Engine) prepareSudo(ctx context.Context, tasks []*Task) error {
	if e.Runtime == nil || !e.Runtime.NeedsSudo() {
		return nil
	}
	if !slices.ContainsFunc(tasks, func(task *Task) bool { return task.RequiresSudo }) {
		return nil
	}
	cmd := exec.CommandContext(ctx, "sudo", "-v")
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}
		return fmt.Errorf("sudo authentication failed: %w:\n%s", err, msg)
	}
	return nil
}

// Selected returns tasks matching selection, topologically sorted.
func (e *Engine) Selected(selection Selection) ([]*Task, error) {
	tasks := slices.Clone(e.Tasks)
	taskIDs := make(map[string]bool)
	for _, task := range tasks {
		taskIDs[task.ID] = true
	}
	var unknown []string
	for _, id := range append(slices.Clone(selection.Only), selection.Skip...) {
		if !taskIDs[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		unknown = slices.Compact(unknown)
		return nil, fmt.Errorf("unknown task id(s): %s", strings.Join(unknown, ", "))
	}
	if len(selection.Only) > 0 {
		tasks = slices.DeleteFunc(tasks, func(task *Task) bool {
			return !slices.Contains(selection.Only, task.ID)
		})
	}
	if len(selection.Skip) > 0 {
		tasks = slices.DeleteFunc(tasks, func(task *Task) bool {
			return slices.Contains(selection.Skip, task.ID)
		})
	}
	if len(selection.Tags) > 0 {
		tasks = slices.DeleteFunc(tasks, func(task *Task) bool {
			return !task.hasAnyTag(selection.Tags)
		})
	}
	if len(tasks) == 0 {
		return nil, errors.New("no tasks selected")
	}
	return SortTasks(tasks)
}

// SortTasks topologically sorts tasks based on their DependsOn field.
func SortTasks(tasks []*Task) ([]*Task, error) {
	inDegree := make(map[string]int)
	graph := make(map[string][]*Task)
	taskMap := make(map[string]*Task)

	for _, t := range tasks {
		taskMap[t.ID] = t
		inDegree[t.ID] = 0
	}

	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if _, ok := taskMap[dep]; ok {
				graph[dep] = append(graph[dep], t)
				inDegree[t.ID]++
			}
		}
	}

	var zeroInDegree []*Task
	for _, t := range tasks {
		if inDegree[t.ID] == 0 {
			zeroInDegree = append(zeroInDegree, t)
		}
	}

	var sorted []*Task
	for len(zeroInDegree) > 0 {
		n := zeroInDegree[0]
		zeroInDegree = zeroInDegree[1:]
		sorted = append(sorted, n)

		for _, m := range graph[n.ID] {
			inDegree[m.ID]--
			if inDegree[m.ID] == 0 {
				zeroInDegree = append(zeroInDegree, m)
			}
		}
	}

	if len(sorted) != len(tasks) {
		return nil, errors.New("cycle detected in task dependencies")
	}

	return sorted, nil
}

func (task *Task) hasAnyTag(tags []string) bool {
	for _, tag := range tags {
		if slices.Contains(task.Tags, tag) {
			return true
		}
	}
	return false
}

func (e *Engine) starlarkTask(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		id              string
		name            string
		tags            *starlark.List
		dependsOn       *starlark.List
		continueOnError bool
		requiresSudo    bool
		run             starlark.Callable
		when            bool = true
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"id", &id,
		"name", &name,
		"run", &run,
		"tags?", &tags,
		"depends_on?", &dependsOn,
		"continue_on_error?", &continueOnError,
		"requires_sudo?", &requiresSudo,
		"when?", &when,
	); err != nil {
		return nil, err
	}
	if !when {
		return starlark.None, nil
	}
	if id == "" {
		return nil, fmt.Errorf("%s: id cannot be empty", b.Name())
	}
	if run == nil {
		return nil, fmt.Errorf("%s: run is required", b.Name())
	}

	var tagStrings []string
	if tags != nil {
		for i := range tags.Len() {
			tag, ok := starlark.AsString(tags.Index(i))
			if !ok {
				return nil, fmt.Errorf("%s: tags[%d] is not a string", b.Name(), i)
			}
			tagStrings = append(tagStrings, tag)
		}
	}

	var depsStrings []string
	if dependsOn != nil {
		for i := range dependsOn.Len() {
			dep, ok := starlark.AsString(dependsOn.Index(i))
			if !ok {
				return nil, fmt.Errorf("%s: depends_on[%d] is not a string", b.Name(), i)
			}
			depsStrings = append(depsStrings, dep)
		}
	}

	if slices.ContainsFunc(e.Tasks, func(task *Task) bool { return task.ID == id }) {
		return nil, fmt.Errorf("%s: duplicate task id %q", b.Name(), id)
	}

	e.Tasks = append(e.Tasks, &Task{
		ID:              id,
		Name:            name,
		Tags:            tagStrings,
		DependsOn:       depsStrings,
		ContinueOnError: continueOnError,
		RequiresSudo:    requiresSudo,
		Run:             run,
	})
	return starlark.None, nil
}

// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

type actionResult struct {
	task        *Task
	taskIndex   int
	action      Action
	actionIndex int
	result      Result
	err         error
}

type execution struct {
	planned  map[string]bool
	summary  Summary
	actions  []actionResult
	failures []failure
}

func (r execution) orderedActions() []actionResult {
	actions := slices.Clone(r.actions)
	slices.SortStableFunc(actions, func(a, b actionResult) int {
		if a.taskIndex != b.taskIndex {
			return a.taskIndex - b.taskIndex
		}
		return a.actionIndex - b.actionIndex
	})
	return actions
}

type taskResult struct {
	task     *Task
	actions  []actionResult
	failures []failure
	failed   bool
}

// execute runs every prepared task through the same dependency-aware
// scheduler. A concurrency limit of one is ordinary scheduler configuration,
// not a separate execution mode.
func (e *Engine) execute(ctx context.Context, tasks []*Task, run execution, opts RunOptions, warnings *warningCollector, taskDone func(*Task)) execution {
	limit := max(opts.Concurrency, 1)
	status := initialTaskStatus(tasks, run.planned, run.failures)
	actions := newActionLimiter(limit)
	results := make(chan taskResult)
	active := 0
	stopping := opts.FailFast && run.summary.Failed > 0

	for _, task := range tasks {
		if !run.planned[task.ID] {
			taskDone(task)
		}
	}

	for {
		started := false
		if !stopping {
			for taskIndex, task := range tasks {
				if active == limit {
					break
				}
				if !run.planned[task.ID] || status[task.ID] != taskPending {
					continue
				}
				dep, waiting := taskDependency(status, task)
				if waiting {
					continue
				}
				if dep != "" {
					err := fmt.Errorf("dependency %s failed", dep)
					run.summary.Failed++
					run.failures = append(run.failures, failure{TaskID: task.ID, TaskName: task.Name, Err: err})
					status[task.ID] = taskFailed
					taskDone(task)
					started = true
					if opts.FailFast && !task.ContinueOnError {
						stopping = true
						break
					}
					continue
				}

				status[task.ID] = taskRunning
				active++
				started = true
				go func() {
					results <- e.executeTask(ctx, task, taskIndex, opts, warnings, actions)
				}()
			}
		}

		if active == 0 {
			if !started {
				break
			}
			continue
		}

		result := <-results
		active--
		run.actions = append(run.actions, result.actions...)
		run.failures = append(run.failures, result.failures...)
		for _, action := range result.actions {
			run.summary.Actions++
			if action.err != nil {
				run.summary.Failed++
				continue
			}
			switch action.result {
			case ResultChange:
				run.summary.Changed++
			case ResultSkip, ResultStop:
				run.summary.Skipped++
			}
		}
		if result.failed {
			status[result.task.ID] = taskFailed
			if opts.FailFast && !result.task.ContinueOnError {
				stopping = true
			}
		} else {
			status[result.task.ID] = taskSucceeded
		}
		taskDone(result.task)
	}

	return run
}

func (e *Engine) executeTask(ctx context.Context, task *Task, taskIndex int, opts RunOptions, warnings *warningCollector, limiter actionLimiter) taskResult {
	run := taskResult{task: task}
	execute := func(actionIndex int) actionResult {
		action := task.Actions[actionIndex]
		result := actionResult{task: task, taskIndex: taskIndex, action: action, actionIndex: actionIndex}
		if err := limiter.acquire(ctx); err != nil {
			result.err = err
			return result
		}
		result.result, result.err = warnings.run(ctx, task, action, opts.DryRun)
		limiter.release()
		return result
	}

	for i := 0; i < len(task.Actions); {
		if !task.Actions[i].Concurrent || opts.Concurrency <= 1 {
			result := execute(i)
			run.add(result)
			i++
			if result.result == ResultStop && !opts.DryRun || result.err != nil && opts.FailFast && !task.ContinueOnError {
				break
			}
			continue
		}

		end := i + 1
		for end < len(task.Actions) && task.Actions[end].Concurrent {
			end++
		}
		results := make(chan actionResult, end-i)
		var group sync.WaitGroup
		for actionIndex := i; actionIndex < end; actionIndex++ {
			group.Go(func() { results <- execute(actionIndex) })
		}
		group.Wait()
		close(results)
		stop := false
		for result := range results {
			run.add(result)
			stop = stop || result.result == ResultStop && !opts.DryRun || result.err != nil && opts.FailFast && !task.ContinueOnError
		}
		if stop {
			break
		}
		i = end
	}
	slices.SortStableFunc(run.actions, func(a, b actionResult) int { return a.actionIndex - b.actionIndex })
	return run
}

func (r *taskResult) add(result actionResult) {
	r.actions = append(r.actions, result)
	if result.err == nil {
		return
	}
	r.failed = true
	r.failures = append(r.failures, failure{
		TaskID:   r.task.ID,
		TaskName: r.task.Name,
		Action:   result.action.description(),
		Err:      result.err,
	})
}

func initialTaskStatus(tasks []*Task, planned map[string]bool, failures []failure) map[string]taskStatus {
	status := make(map[string]taskStatus)
	for _, task := range tasks {
		status[task.ID] = taskPending
		if planned[task.ID] {
			continue
		}
		if _, ok := taskFailure(failures, task); ok {
			status[task.ID] = taskFailed
		} else {
			status[task.ID] = taskSucceeded
		}
	}
	return status
}

func taskDependency(status map[string]taskStatus, task *Task) (failed string, waiting bool) {
	for _, dep := range task.DependsOn {
		switch status[dep] {
		case taskPending, taskRunning:
			waiting = true
		case taskFailed:
			return dep, false
		}
	}
	return "", waiting
}

type taskStatus int

const (
	taskPending taskStatus = iota
	taskRunning
	taskSucceeded
	taskFailed
)

type actionLimiter struct {
	ch chan struct{}
}

func newActionLimiter(limit int) actionLimiter {
	return actionLimiter{ch: make(chan struct{}, max(limit, 1))}
}

func (l actionLimiter) acquire(ctx context.Context) error {
	select {
	case l.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l actionLimiter) release() {
	<-l.ch
}

// colorCode is tiny instead of using a terminal styling package; boot only
// needs a few stable labels in human output.
type colorCode string

const (
	colorBold   colorCode = "\033[1m"
	colorGreen  colorCode = "\033[32m"
	colorYellow colorCode = "\033[33m"
	colorRed    colorCode = "\033[31m"
)

func (e *Engine) color(s string, code colorCode) string {
	if !e.colorEnabled() {
		return s
	}
	return string(code) + s + "\033[0m"
}

func (e *Engine) colorEnabled() bool {
	if e.Runtime == nil {
		return true
	}
	return e.Runtime.Color
}

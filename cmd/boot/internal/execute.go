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
	description string
	result      Result
	err         error
}

type taskRun struct {
	task         *Task
	status       taskStatus
	err          error
	actions      []actionResult
	consentCount int
}

func (r *taskRun) executionActions() []actionResult { return r.actions[r.consentCount:] }

type taskStatus int

const (
	taskUnprepared taskStatus = iota
	taskSkipped
	taskPrepareFailed
	taskPending
	taskRunning
	taskSucceeded
	taskFailed
)

func execute(ctx context.Context, runs []taskRun, opts RunOptions, warnings *warningCollector, progress *runProgress) {
	limit := max(opts.Concurrency, 1)
	limiter := make(actionLimiter, limit)
	results := make(chan *taskRun)
	byID := make(map[string]*taskRun, len(runs))
	for i := range runs {
		r := &runs[i]
		byID[r.task.ID] = r
		if r.status != taskPending {
			progress.taskDone()
		}
	}

	active := 0
	stopping := false
	for {
		if !stopping {
			for i := range runs {
				if active == limit {
					break
				}
				r := &runs[i]
				if r.status != taskPending {
					continue
				}
				dep, waiting := taskDependency(byID, r.task)
				if waiting {
					continue
				}

				if dep != "" {
					r.err = fmt.Errorf("dependency %s failed", dep)
					r.status = taskFailed
					progress.taskDone()
					if opts.FailFast && !r.task.ContinueOnError {
						stopping = true
						break
					}
					continue
				}

				r.status = taskRunning
				active++
				go func() {
					r.execute(ctx, opts, warnings, limiter, progress)
					results <- r
				}()
			}
		}

		if active == 0 {
			return
		}

		r := <-results
		active--
		r.status = taskSucceeded
		if slices.ContainsFunc(r.executionActions(), func(a actionResult) bool { return a.err != nil }) {
			r.status = taskFailed
			stopping = stopping || opts.FailFast && !r.task.ContinueOnError
		}
		progress.taskDone()
	}
}

func (r *taskRun) execute(ctx context.Context, opts RunOptions, warnings *warningCollector, limiter actionLimiter, progress *runProgress) {
	actions := r.task.Actions
	for start := 0; start < len(actions); {
		end := start + 1
		if opts.Concurrency > 1 && actions[start].Concurrent {
			for end < len(actions) && actions[end].Concurrent {
				end++
			}
		}

		offset := len(r.actions)
		r.actions = append(r.actions, make([]actionResult, end-start)...)
		batch := r.actions[offset:]
		if len(batch) == 1 {
			batch[0] = r.apply(ctx, &actions[start], opts.DryRun, warnings, limiter, progress)
		} else {
			var group sync.WaitGroup
			for i := range batch {
				group.Go(func() {
					batch[i] = r.apply(ctx, &actions[start+i], opts.DryRun, warnings, limiter, progress)
				})
			}
			group.Wait()
		}

		for _, action := range batch {
			if action.result == ResultStop && !opts.DryRun ||
				action.err != nil && opts.FailFast && !r.task.ContinueOnError {
				return
			}
		}
		start = end
	}
}

func (r *taskRun) apply(ctx context.Context, action *Action, dryRun bool, warnings *warningCollector, limiter actionLimiter, progress *runProgress) actionResult {
	if err := limiter.acquire(ctx); err != nil {
		return actionResult{description: action.description(), err: err}
	}
	defer limiter.release()
	progress.startAction(action)
	defer progress.finishAction(action)
	return r.runAction(ctx, *action, dryRun, warnings)
}

func (r *taskRun) runAction(ctx context.Context, action Action, dryRun bool, warnings *warningCollector) actionResult {
	result, err := warnings.run(ctx, r.task, action, dryRun)
	return actionResult{description: action.description(), result: result, err: err}
}

func taskDependency(byID map[string]*taskRun, task *Task) (failed string, waiting bool) {
	for _, dep := range task.DependsOn {
		switch byID[dep].status {
		case taskPending, taskRunning:
			waiting = true
		case taskPrepareFailed, taskFailed:
			return dep, false
		}
	}
	return "", waiting
}

type actionLimiter chan struct{}

func (l actionLimiter) acquire(ctx context.Context) error {
	select {
	case l <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l actionLimiter) release() {
	<-l
}

// colorCode is tiny instead of using a terminal styling package; boot only
// needs a few stable labels in human output.
type colorCode string

const (
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

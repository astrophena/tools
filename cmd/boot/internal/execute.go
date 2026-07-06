// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import "context"

// initialTaskStatus seeds dependency state after prepare.
//
// Planned tasks start as pending and will be marked by the apply path. Unplanned
// tasks are classified immediately: a task that failed while evaluating its
// Starlark body is a failed dependency, while a task skipped by consent is a
// successful terminal dependency because there is nothing left to apply.
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

// failedDependency reports the first declared dependency that failed.
//
// Tasks are already topologically sorted, so the sequential executor can call
// this before each task. The parallel executor waits on doneCh before consulting
// the same status map.
func failedDependency(status map[string]taskStatus, task *Task) (string, bool) {
	for _, dep := range task.DependsOn {
		if status[dep] == taskFailed {
			return dep, true
		}
	}
	return "", false
}

// colorCode is tiny instead of using a terminal styling package; boot only
// needs a few stable labels in human output.
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
	if e.Runtime == nil {
		return true
	}
	return e.Runtime.Color
}

const (
	taskPending taskStatus = iota
	taskSucceeded
	taskFailed
)

type taskStatus int

// actionLimiter is a cancellation-aware semaphore used for both task and action
// concurrency. It is deliberately small so the execution paths can express their
// scheduling policy directly instead of hiding it behind a worker-pool type.
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

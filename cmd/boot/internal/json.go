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
)

// jsonRun is the complete machine-readable report for a run.
//
// JSON mode is intentionally collected and emitted once at the end instead of
// streaming. That keeps stdout valid JSON even when an action fails midway and
// makes it safe for other programs to parse boot output without watching event
// boundaries.
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

// runJSON executes tasks in deterministic order and suppresses module chatter.
//
// Human modes allow check modules to print details through Runtime.Stdout. JSON
// mode redirects that writer to io.Discard while actions run so stdout contains
// only the final JSON object. If a caller needs verbose diagnostics, it should
// run without -json.
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
	planned, summary, failures, stopOnError := e.prepare(ctx, tasks, opts)
	status := initialTaskStatus(tasks, planned, failures)
	if err := newSudoPrompter(e).prepare(ctx, io.Discard, tasks); err != nil {
		return err
	}
	report := jsonRun{DryRun: opts.DryRun, Summary: summary}
	for _, f := range failures {
		report.Actions = append(report.Actions, jsonAction{TaskID: f.TaskID, TaskName: f.TaskName, Error: f.Err.Error()})
	}

	if stopOnError && opts.FailFast {
		if err := json.NewEncoder(w).Encode(report); err != nil {
			return err
		}
		return errors.New("one or more tasks failed")
	}

	for _, task := range tasks {
		if !planned[task.ID] {
			continue
		}
		if dep, failed := failedDependency(status, task); failed {
			err := fmt.Errorf("dependency %s failed", dep)
			report.Summary.Failed++
			report.Actions = append(report.Actions, jsonAction{TaskID: task.ID, TaskName: task.Name, Error: err.Error()})
			status[task.ID] = taskFailed
			if opts.FailFast && !task.ContinueOnError {
				break
			}
			continue
		}
		status[task.ID] = taskSucceeded
		stop := false
		for _, action := range task.Actions {
			report.Summary.Actions++
			result, err := action.Apply(ctx, opts.DryRun)
			item := jsonAction{TaskID: task.ID, TaskName: task.Name, Summary: action.Summary, Result: result}
			if err != nil {
				status[task.ID] = taskFailed
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

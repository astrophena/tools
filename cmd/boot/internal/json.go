// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"encoding/json"
	"io"
)

// jsonRun is the complete machine-readable report for a run.
//
// JSON mode is collected and emitted once at the end instead of streaming. That
// keeps stdout valid JSON even when an action fails midway and makes it safe
// for other programs to parse boot output without watching event boundaries.
type jsonRun struct {
	DryRun   bool         `json:"dry_run"`
	Summary  Summary      `json:"summary"`
	Actions  []jsonAction `json:"actions"`
	Warnings []warning    `json:"warnings,omitempty"`
}

type jsonAction struct {
	TaskID   string `json:"task_id"`
	TaskName string `json:"task_name"`
	Summary  string `json:"summary,omitempty"`
	Result   Result `json:"result,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (e *Engine) printJSON(w io.Writer, run execution, warnings []warning, dryRun bool) error {
	report := jsonRun{DryRun: dryRun, Summary: run.summary, Warnings: warnings}
	for _, failure := range run.failures {
		if failure.Action == "" {
			report.Actions = append(report.Actions, jsonAction{
				TaskID: failure.TaskID, TaskName: failure.TaskName, Error: failure.Err.Error(),
			})
		}
	}
	for _, action := range run.orderedActions() {
		item := jsonAction{
			TaskID: action.task.ID, TaskName: action.task.Name,
			Summary: action.action.description(), Result: action.result,
		}
		if action.err != nil {
			item.Error = action.err.Error()
		}
		report.Actions = append(report.Actions, item)
	}
	return json.NewEncoder(w).Encode(report)
}

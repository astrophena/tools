// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"encoding/json"
	"io"
)

// jsonRun is written after all results have been collected.
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

func printJSON(w io.Writer, runs []taskRun, summary Summary, warnings []warning, dryRun bool) error {
	report := jsonRun{
		DryRun:   dryRun,
		Summary:  summary,
		Warnings: warnings,
	}
	for i := range runs {
		r := &runs[i]
		if r.err != nil {
			report.Actions = append(report.Actions, jsonAction{
				TaskID:   r.task.ID,
				TaskName: r.task.Name,
				Error:    r.err.Error(),
			})
		}
	}
	for i := range runs {
		r := &runs[i]
		for j, action := range r.actions {
			if j < r.consentCount && action.err == nil {
				continue
			}
			item := jsonAction{
				TaskID:   r.task.ID,
				TaskName: r.task.Name,
				Summary:  action.description,
				Result:   action.result,
			}
			if action.err != nil {
				item.Error = action.err.Error()
			}
			report.Actions = append(report.Actions, item)
		}
	}
	return json.NewEncoder(w).Encode(report)
}

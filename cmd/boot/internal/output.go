// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"fmt"
	"io"
	"os"
	"strings"
)

type failure struct {
	TaskID   string
	TaskName string
	Action   string
	Err      error
}

func (e *Engine) printPlan(w io.Writer, runs []taskRun, summary Summary, verbose bool) {
	for i := range runs {
		r := &runs[i]
		if r.status == taskUnprepared || r.status == taskSkipped {
			continue
		}
		fmt.Fprintf(w, "[%d/%d] Planning task %s: %s\n", i+1, len(runs), r.task.ID, r.task.Name)
		if r.status == taskPrepareFailed {
			fmt.Fprintf(w, "%s %s: %v\n", e.color("fail", colorRed), r.task.ID, r.err)
		}
		for _, action := range r.executionActions() {
			switch {
			case action.err != nil:
				fmt.Fprintf(w, "%s %s: %s: %v\n", e.color("fail", colorRed), r.task.ID, action.description, action.err)
			case action.result == ResultChange:
				fmt.Fprintf(w, "%s %s: %s\n", e.color("change", colorGreen), r.task.ID, action.description)
			case verbose:
				fmt.Fprintf(w, "skip %s: %s\n", r.task.ID, action.description)
			}
		}
	}
	fmt.Fprintf(w, "summary: %d tasks, %d actions, %d would change, %d skipped, %d warnings, %d failed\n",
		summary.Tasks, summary.Actions, summary.Changed, summary.Skipped, summary.Warnings, summary.Failed)
}

func (e *Engine) printVerbose(w io.Writer, runs []taskRun) {
	for i := range runs {
		r := &runs[i]
		if r.status < taskPending {
			continue
		}
		fmt.Fprintf(w, "[%d/%d] Applying task %s: %s\n", i+1, len(runs), r.task.ID, r.task.Name)
		for _, action := range r.executionActions() {
			if action.err != nil {
				continue
			}
			label := string(action.result)
			if action.result == ResultChange {
				label = e.color(label, colorGreen)
			}
			fmt.Fprintf(w, "%s %s: %s\n", label, r.task.ID, action.description)
		}
	}
}

func (e *Engine) printReport(w io.Writer, summary Summary) {
	fmt.Fprintf(w, "%s\n", e.color("Report:", colorBold))
	fmt.Fprintf(w, "  Boot ran %d %s and checked %d %s.\n",
		summary.Tasks,
		plural(summary.Tasks, "task", "tasks"),
		summary.Actions,
		plural(summary.Actions, "action", "actions"),
	)
	fmt.Fprintf(w, "  It %s %s and skipped %s.\n",
		"changed",
		e.color(fmt.Sprintf("%d %s", summary.Changed, plural(summary.Changed, "action", "actions")), colorGreen),
		fmt.Sprintf("%d %s", summary.Skipped, plural(summary.Skipped, "action", "actions")),
	)
	if summary.Warnings > 0 {
		fmt.Fprintf(w, "  It reported %s.\n", e.color(fmt.Sprintf("%d %s", summary.Warnings, plural(summary.Warnings, "warning", "warnings")), colorYellow))
	}
	if summary.Failed == 0 {
		fmt.Fprintf(w, "  %s\n", e.color("No actions failed.", colorGreen))
		return
	}
	fmt.Fprintf(w, "  %s\n", e.color(fmt.Sprintf("%d %s failed.", summary.Failed, plural(summary.Failed, "action", "actions")), colorRed))
}

func (e *Engine) printWarnings(w io.Writer, warnings []warning) {
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintf(w, "%s\n", e.color("Warnings:", colorYellow))
	for _, warning := range warnings {
		fmt.Fprintf(w, "%s\n", e.color(fmt.Sprintf("  Task: %s (%s)", warning.TaskID, warning.TaskName), colorYellow))
		fmt.Fprintf(w, "%s\n", e.color("    Action: "+warning.Action, colorYellow))
		lines := strings.Split(warning.Message, "\n")
		fmt.Fprintf(w, "%s\n", e.color("    Warning: "+lines[0], colorYellow))
		for _, line := range lines[1:] {
			fmt.Fprintf(w, "%s\n", e.color("      "+line, colorYellow))
		}
	}
}

func (e *Engine) printFailures(w io.Writer, runs []taskRun, failed int) error {
	if failed == 0 {
		return nil
	}
	fmt.Fprintf(w, "%s\n", e.color("errors:", colorRed))
	for i := range runs {
		r := &runs[i]
		f := failure{TaskID: r.task.ID, TaskName: r.task.Name, Err: r.err}
		if f.Err != nil {
			if err := e.printFailure(w, f); err != nil {
				return err
			}
		}
		for _, action := range r.actions {
			if action.err == nil {
				continue
			}
			f.Action = action.description
			f.Err = action.err
			if err := e.printFailure(w, f); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) printFailure(w io.Writer, f failure) error {
	logPath, err := writeFailureLog(f)
	if err != nil {
		return err
	}
	if f.TaskID != "" {
		fmt.Fprintf(w, "%s\n", e.color(fmt.Sprintf("  task: %s (%s)", f.TaskID, f.TaskName), colorRed))
	}
	if f.Action != "" {
		fmt.Fprintf(w, "%s\n", e.color("    action: "+f.Action, colorRed))
	}
	fmt.Fprintf(w, "%s\n", e.color("    error: "+firstLine(f.Err.Error()), colorRed))
	fmt.Fprintf(w, "%s\n", e.color("    log: "+logPath, colorRed))
	if output := restLines(f.Err.Error()); output != "" {
		fmt.Fprintf(w, "%s\n", e.color("    output:", colorRed))
		for line := range strings.SplitSeq(output, "\n") {
			fmt.Fprintf(w, "%s\n", e.color("      "+line, colorRed))
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

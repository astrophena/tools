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

func taskFailure(failures []failure, task *Task) (failure, bool) {
	for _, failure := range failures {
		if failure.TaskID == task.ID && failure.Action == "" {
			return failure, true
		}
	}
	return failure{}, false
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
	fmt.Fprintf(w, "%s\n", e.color("warnings:", colorYellow))
	for _, warning := range warnings {
		fmt.Fprintf(w, "%s\n", e.color(fmt.Sprintf("  task: %s (%s)", warning.TaskID, warning.TaskName), colorYellow))
		fmt.Fprintf(w, "%s\n", e.color("    action: "+warning.Action, colorYellow))
		lines := strings.Split(warning.Message, "\n")
		fmt.Fprintf(w, "%s\n", e.color("    warning: "+lines[0], colorYellow))
		for _, line := range lines[1:] {
			fmt.Fprintf(w, "%s\n", e.color("      "+line, colorYellow))
		}
	}
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

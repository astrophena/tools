// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func (e *Engine) printPlan(w io.Writer, runs []taskRun, verbose bool) {
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

func (e *Engine) printSummary(w io.Writer, summary Summary, dryRun bool) {
	changed := "changed"
	if dryRun {
		changed = "would change"
	}

	parts := []string{
		fmt.Sprintf("%d %s", summary.Tasks, plural(summary.Tasks, "task", "tasks")),
		e.color(fmt.Sprintf("%d %s", summary.Changed, changed), colorGreen),
		fmt.Sprintf("%d skipped", summary.Skipped),
	}
	if summary.Warnings > 0 {
		warnings := fmt.Sprintf("%d %s", summary.Warnings, plural(summary.Warnings, "warning", "warnings"))
		parts = append(parts, e.color(warnings, colorYellow))
	}
	if summary.Failed > 0 {
		failures := fmt.Sprintf("%d %s", summary.Failed, plural(summary.Failed, "failure", "failures"))
		parts = append(parts, e.color(failures, colorRed))
	}
	fmt.Fprintln(w, strings.Join(parts, " · "))
}

func (e *Engine) printWarnings(w io.Writer, warnings []warning) {
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintln(w, e.color("Warnings", colorYellow))
	for i, warning := range warnings {
		if i == 0 || warning.TaskID != warnings[i-1].TaskID || warning.Action != warnings[i-1].Action {
			heading := warning.TaskID
			if warning.Action != "" {
				heading += " · " + warning.Action
			}
			fmt.Fprintln(w, "  "+heading)
		}
		for line := range strings.SplitSeq(warning.Message, "\n") {
			fmt.Fprintln(w, "    "+line)
		}
	}
}

func (e *Engine) printFailures(w io.Writer, runs []taskRun, failed int) {
	if failed == 0 {
		return
	}
	fmt.Fprintln(w, e.color("Failures", colorRed))
	for i := range runs {
		r := &runs[i]
		if r.err != nil {
			printFailure(w, r.task.ID, "", r.err)
		}
		for _, action := range r.actions {
			if action.err != nil {
				printFailure(w, r.task.ID, action.description, action.err)
			}
		}
	}
}

func printFailure(w io.Writer, taskID, action string, err error) {
	heading := taskID
	if action != "" {
		heading += " · " + action
	}
	fmt.Fprintln(w, "  "+heading)

	text := strings.TrimSpace(err.Error())
	const previewLines = 8
	lines := strings.Split(text, "\n")
	var logPath string
	if len(lines) > previewLines {
		if path, err := writeFailureLog(heading, text); err == nil {
			logPath = path
			lines = lines[:previewLines]
		}
	}
	for _, line := range lines {
		fmt.Fprintln(w, "    "+line)
	}
	if logPath != "" {
		fmt.Fprintln(w, "    log: "+logPath)
	}
}

func writeFailureLog(heading, message string) (string, error) {
	file, err := os.CreateTemp("", "boot-error-*.log")
	if err != nil {
		return "", err
	}
	_, err = fmt.Fprintln(file, heading+"\n"+message)
	if err := errors.Join(err, file.Close()); err != nil {
		os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

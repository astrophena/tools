// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// sudoPrompter performs one upfront sudo validation per run.
//
// Modules mark Actions with RequiresSudo while task bodies are being prepared.
// Prompting after preparation lets boot tell the user exactly which tasks/actions
// need privilege, and avoids interleaving sudo password prompts with progress bar
// output or concurrent action execution.
type sudoPrompter struct {
	engine   *Engine
	prepared bool
}

func newSudoPrompter(engine *Engine) *sudoPrompter {
	return &sudoPrompter{engine: engine}
}

func (p *sudoPrompter) prepare(ctx context.Context, w io.Writer, tasks []*Task) error {
	if p.prepared || p.engine.Runtime == nil || !p.engine.Runtime.NeedsSudo() {
		return nil
	}
	reasons := sudoReasons(tasks)
	if len(reasons) == 0 {
		return nil
	}
	if w == nil {
		w = io.Discard
	}
	fmt.Fprintln(w, "Boot requests administrator permissions to run the following tasks and actions:")
	for _, reason := range reasons {
		fmt.Fprintf(w, "  - %s\n", reason)
	}
	cmd := exec.CommandContext(ctx, "sudo", "-v")
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}
		return fmt.Errorf("sudo authentication failed: %w:\n%s", err, msg)
	}
	p.prepared = true
	return nil
}

// sudoReasons prefers action-level reasons over task-level reasons.
// Task.RequiresSudo is still useful for opaque recipe tasks, but action summaries
// are more precise once module calls have expanded the task body.
func sudoReasons(tasks []*Task) []string {
	var reasons []string
	for _, task := range tasks {
		actions := sudoActionReasons(task)
		if len(actions) == 0 && task.RequiresSudo {
			reasons = append(reasons, fmt.Sprintf("task %s: %s", task.ID, task.Name))
		}
		reasons = append(reasons, actions...)
	}
	return reasons
}

func sudoActionReasons(task *Task) []string {
	var reasons []string
	for _, action := range task.Actions {
		if action.RequiresSudo {
			reasons = append(reasons, fmt.Sprintf("%s: %s", task.ID, action.Summary))
		}
	}
	return reasons
}

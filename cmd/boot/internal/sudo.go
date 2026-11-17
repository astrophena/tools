// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// prepareSudo authenticates before execution so prompts stay out of parallel output.
func (e *Engine) prepareSudo(ctx context.Context, w io.Writer, runs []taskRun) error {
	if e.Runtime == nil || !e.Runtime.NeedsSudo() {
		return nil
	}
	reasons := sudoReasons(runs)
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

	return authenticateSudo(ctx, e.Runtime, w)
}

func authenticateSudo(ctx context.Context, rt *Runtime, w io.Writer) error {
	out, err := runCachedSudo(ctx)
	if err == nil {
		return nil
	}
	if !rt.Interactive {
		return sudoAuthError(err, out)
	}

	out, err = runInteractiveSudo(ctx, rt.Stdin, w)
	if err != nil {
		return sudoAuthError(err, out)
	}
	return nil
}

func runCachedSudo(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "sudo", "-n", "-v")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.Bytes(), err
}

func runInteractiveSudo(ctx context.Context, in io.Reader, w io.Writer) ([]byte, error) {
	if in == nil {
		in = os.Stdin
	}
	out := &sudoOutput{
		w: w,
	}
	cmd := exec.CommandContext(ctx, "sudo", "-v")
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	b := out.buf.Bytes()
	if len(b) == 0 || b[len(b)-1] != '\n' {
		fmt.Fprintln(w)
	}
	return b, err
}

func sudoAuthError(err error, out []byte) error {
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return fmt.Errorf("sudo authentication failed: %w", err)
	}
	return fmt.Errorf("sudo authentication failed: %w:\n%s", err, msg)
}

type sudoOutput struct {
	w io.Writer

	buf bytes.Buffer
}

func (w *sudoOutput) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.buf.Write(p[:n])
	return n, err
}

// sudoReasons prefers action-level reasons over task-level reasons.
// Task.RequiresSudo is still useful for opaque recipe tasks, but action summaries
// are more precise once module calls have expanded the task body.
func sudoReasons(runs []taskRun) []string {
	var reasons []string
	for i := range runs {
		r := &runs[i]
		if r.status != taskPending {
			continue
		}
		task := r.task
		start := len(reasons)
		for _, action := range r.prepared {
			if action.RequiresSudo {
				reasons = append(reasons, fmt.Sprintf("%s: %s", task.ID, action.Summary))
			}
		}
		if len(reasons) == start && task.RequiresSudo {
			reasons = append(reasons, fmt.Sprintf("task %s: %s", task.ID, task.Name))
		}
	}
	return reasons
}

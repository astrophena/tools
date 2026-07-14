// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"

	"go.astrophena.name/base/ctxkey"
)

var warningSinkContextKey = ctxkey.New[func(string)]("boot.warningSink", nil)

// WithWarningSink returns a context that sends warnings to sink.
func WithWarningSink(ctx context.Context, sink func(string)) context.Context {
	return warningSinkContextKey.WithValue(ctx, sink)
}

// Warn reports a non-fatal problem discovered while running an action.
func Warn(ctx context.Context, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	if sink := warningSinkContextKey.Value(ctx); sink != nil {
		sink(message)
	}
}

type warning struct {
	TaskID   string `json:"task_id"`
	TaskName string `json:"task_name"`
	Action   string `json:"action"`
	Message  string `json:"message"`
}

type warningCollector struct {
	mu       sync.Mutex
	warnings []warning
}

func (c *warningCollector) run(ctx context.Context, task *Task, action Action, dryRun bool) (Result, error) {
	ctx = WithWarningSink(ctx, func(message string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.warnings = append(c.warnings, warning{
			TaskID:   task.ID,
			TaskName: task.Name,
			Action:   action.description(),
			Message:  message,
		})
	})
	return action.Apply(ctx, dryRun)
}

func (c *warningCollector) all() []warning {
	c.mu.Lock()
	defer c.mu.Unlock()
	warnings := slices.Clone(c.warnings)
	slices.SortStableFunc(warnings, func(a, b warning) int {
		if n := cmp.Compare(a.TaskID, b.TaskID); n != 0 {
			return n
		}
		if n := cmp.Compare(a.Action, b.Action); n != 0 {
			return n
		}
		return cmp.Compare(a.Message, b.Message)
	})
	return warnings
}

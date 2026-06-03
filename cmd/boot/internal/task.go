// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.starlark.net/starlark"
)

// Selected returns tasks matching selection, topologically sorted.
//
// Selection is deliberately strict. If a user selects a task but filters out one
// of its declared dependencies, boot returns an error instead of treating that
// dependency as already satisfied. This makes partial runs explicit and avoids a
// common infrastructure-footgun: applying a dependent task against a host whose
// prerequisite task was never checked in this run.
func (e *Engine) Selected(selection Selection) ([]*Task, error) {
	tasks := slices.Clone(e.Tasks)
	taskIDs := make(map[string]bool)
	for _, task := range tasks {
		taskIDs[task.ID] = true
	}
	var unknown []string
	for _, id := range append(slices.Clone(selection.Only), selection.Skip...) {
		if !taskIDs[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		unknown = slices.Compact(unknown)
		return nil, fmt.Errorf("unknown task id(s): %s", strings.Join(unknown, ", "))
	}
	if len(selection.Only) > 0 {
		tasks = slices.DeleteFunc(tasks, func(task *Task) bool {
			return !slices.Contains(selection.Only, task.ID)
		})
	}
	if len(selection.Skip) > 0 {
		tasks = slices.DeleteFunc(tasks, func(task *Task) bool {
			return slices.Contains(selection.Skip, task.ID)
		})
	}
	if len(selection.Tags) > 0 {
		tasks = slices.DeleteFunc(tasks, func(task *Task) bool {
			return !task.hasAnyTag(selection.Tags)
		})
	}
	if len(tasks) == 0 {
		return nil, errors.New("no tasks selected")
	}
	if err := validateSelectedDependencies(tasks); err != nil {
		return nil, err
	}
	return SortTasks(tasks)
}

// validateSelectedDependencies checks the graph after user filters are applied.
// It only sees selected tasks; unknown task IDs are rejected earlier against the
// full recipe task list.
func validateSelectedDependencies(tasks []*Task) error {
	selected := make(map[string]bool)
	for _, task := range tasks {
		selected[task.ID] = true
	}
	missingByTask := make(map[string][]string)
	for _, task := range tasks {
		for _, dep := range task.DependsOn {
			if !selected[dep] {
				missingByTask[task.ID] = append(missingByTask[task.ID], dep)
			}
		}
	}
	if len(missingByTask) == 0 {
		return nil
	}
	var parts []string
	for taskID, deps := range missingByTask {
		slices.Sort(deps)
		deps = slices.Compact(deps)
		parts = append(parts, fmt.Sprintf("%s depends on unselected task(s): %s", taskID, strings.Join(deps, ", ")))
	}
	slices.Sort(parts)
	return fmt.Errorf("selected task dependencies are incomplete: %s", strings.Join(parts, "; "))
}

// SortTasks topologically sorts tasks based on their DependsOn field.
//
// The sort is stable enough for human output: root tasks enter the queue in the
// recipe registration order, and dependents are appended as their last selected
// dependency is satisfied. Missing dependencies are ignored here because
// Selected validates them before sorting; tests call SortTasks directly, so this
// function remains focused on cycle detection and ordering.
func SortTasks(tasks []*Task) ([]*Task, error) {
	inDegree := make(map[string]int)
	graph := make(map[string][]*Task)
	taskMap := make(map[string]*Task)

	for _, t := range tasks {
		taskMap[t.ID] = t
		inDegree[t.ID] = 0
	}

	for _, t := range tasks {
		for _, dep := range t.DependsOn {
			if _, ok := taskMap[dep]; ok {
				graph[dep] = append(graph[dep], t)
				inDegree[t.ID]++
			}
		}
	}

	var zeroInDegree []*Task
	for _, t := range tasks {
		if inDegree[t.ID] == 0 {
			zeroInDegree = append(zeroInDegree, t)
		}
	}

	var sorted []*Task
	for len(zeroInDegree) > 0 {
		n := zeroInDegree[0]
		zeroInDegree = zeroInDegree[1:]
		sorted = append(sorted, n)

		for _, m := range graph[n.ID] {
			inDegree[m.ID]--
			if inDegree[m.ID] == 0 {
				zeroInDegree = append(zeroInDegree, m)
			}
		}
	}

	if len(sorted) != len(tasks) {
		return nil, errors.New("cycle detected in task dependencies")
	}

	return sorted, nil
}

func (task *Task) hasAnyTag(tags []string) bool {
	for _, tag := range tags {
		if slices.Contains(task.Tags, tag) {
			return true
		}
	}
	return false
}

// starlarkTask implements the recipe-level task(...) builtin.
//
// The builtin records metadata and the Starlark callable but does not run the
// callable. Deferring execution until plan/apply lets command-line task filters
// work without evaluating every machine-specific task body.
func (e *Engine) starlarkTask(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		id              string
		name            string
		tags            *starlark.List
		dependsOn       *starlark.List
		continueOnError bool
		requiresSudo    bool
		run             starlark.Callable
		when            bool = true
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"id", &id,
		"name", &name,
		"run", &run,
		"tags?", &tags,
		"depends_on?", &dependsOn,
		"continue_on_error?", &continueOnError,
		"requires_sudo?", &requiresSudo,
		"when?", &when,
	); err != nil {
		return nil, err
	}
	if !when {
		return starlark.None, nil
	}
	if id == "" {
		return nil, fmt.Errorf("%s: id cannot be empty", b.Name())
	}
	if run == nil {
		return nil, fmt.Errorf("%s: run is required", b.Name())
	}

	var tagStrings []string
	if tags != nil {
		for i := range tags.Len() {
			tag, ok := starlark.AsString(tags.Index(i))
			if !ok {
				return nil, fmt.Errorf("%s: tags[%d] is not a string", b.Name(), i)
			}
			tagStrings = append(tagStrings, tag)
		}
	}

	var depsStrings []string
	if dependsOn != nil {
		for i := range dependsOn.Len() {
			dep, ok := starlark.AsString(dependsOn.Index(i))
			if !ok {
				return nil, fmt.Errorf("%s: depends_on[%d] is not a string", b.Name(), i)
			}
			depsStrings = append(depsStrings, dep)
		}
	}

	if slices.ContainsFunc(e.Tasks, func(task *Task) bool { return task.ID == id }) {
		return nil, fmt.Errorf("%s: duplicate task id %q", b.Name(), id)
	}

	e.Tasks = append(e.Tasks, &Task{
		ID:              id,
		Name:            name,
		Tags:            tagStrings,
		DependsOn:       depsStrings,
		ContinueOnError: continueOnError,
		RequiresSudo:    requiresSudo,
		Run:             run,
	})
	return starlark.None, nil
}

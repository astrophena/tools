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

// Selected returns matching tasks in dependency order. It rejects selections
// that omit a selected task's dependencies.
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

// SortTasks orders tasks after their dependencies, retaining recipe order among
// tasks with no dependencies.
func SortTasks(tasks []*Task) ([]*Task, error) {
	inDegree := make(map[string]int, len(tasks))
	graph := make(map[string][]*Task)
	for _, task := range tasks {
		inDegree[task.ID] = 0
	}
	for _, task := range tasks {
		for _, dep := range task.DependsOn {
			if _, ok := inDegree[dep]; ok {
				graph[dep] = append(graph[dep], task)
				inDegree[task.ID]++
			}
		}
	}

	var ready []*Task
	for _, task := range tasks {
		if inDegree[task.ID] == 0 {
			ready = append(ready, task)
		}
	}

	for i := 0; i < len(ready); i++ {
		for _, next := range graph[ready[i].ID] {
			inDegree[next.ID]--
			if inDegree[next.ID] == 0 {
				ready = append(ready, next)
			}
		}
	}
	if len(ready) != len(tasks) {
		return nil, errors.New("cycle detected in task dependencies")
	}
	return ready, nil
}

func (task *Task) hasAnyTag(tags []string) bool {
	for _, tag := range tags {
		if slices.Contains(task.Tags, tag) {
			return true
		}
	}
	return false
}

// starlarkTask registers a task so selection can run before its body.
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

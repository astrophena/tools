// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import "go.starlark.net/starlark"

func testTask(id string, actions ...Action) *Task {
	return &Task{
		ID:   id,
		Name: id,
		Run: starlark.NewBuiltin(id, func(thread *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			for _, action := range actions {
				AddAction(thread, action)
			}
			return starlark.None, nil
		}),
	}
}

// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"fmt"
	"os"

	"go.starlark.net/starlark"
)

const actionsKey = "boot:actions"

// CollectActions directs actions emitted by a Starlark thread to dst.
func CollectActions(thread *starlark.Thread, dst *[]Action) {
	thread.SetLocal(actionsKey, dst)
}

// AddAction appends an action to the thread's current task.
func AddAction(thread *starlark.Thread, action Action) {
	dst := thread.Local(actionsKey).(*[]Action)
	*dst = append(*dst, action)
}

// TaskBuiltin returns a builtin that can only be called from a task.
func TaskBuiltin(name string, fn func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error)) *starlark.Builtin {
	return starlark.NewBuiltin(name, func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if thread.Local(actionsKey) == nil {
			return nil, fmt.Errorf("%s: can only be called from a task", b.Name())
		}
		return fn(thread, b, args, kwargs)
	})
}

// FileMode validates an integer Starlark file mode and returns its permission bits.
func FileMode(name string, mode int) (os.FileMode, error) {
	if mode < 0 || mode > 0o7777 {
		return 0, fmt.Errorf("%s must be between 0o0000 and 0o7777", name)
	}
	return os.FileMode(mode).Perm(), nil
}

// NonEmpty validates a required string argument.
func NonEmpty(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", name)
	}
	return nil
}

// StringList converts a Starlark list to strings, omitting empty values.
func StringList(name string, list *starlark.List) ([]string, error) {
	if list == nil {
		return nil, fmt.Errorf("%s list is required", name)
	}
	names := make([]string, 0, list.Len())
	for i := range list.Len() {
		item, ok := starlark.AsString(list.Index(i))
		if !ok {
			return nil, fmt.Errorf("%s[%d] is not a string", name, i)
		}
		if item != "" {
			names = append(names, item)
		}
	}
	return names, nil
}

// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"fmt"
	"os"

	"go.starlark.net/starlark"
)

// Common Starlark utilities.

// RequireTask reports an error when a builtin that emits actions is called outside a task.
func RequireTask(thread *starlark.Thread, b *starlark.Builtin) error {
	if !InTask(thread) {
		return fmt.Errorf("%s: can only be called from a task", b.Name())
	}
	return nil
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

// StringList converts [starlark.List] to []string.
func StringList(name string, list *starlark.List) ([]string, error) {
	if list == nil {
		return nil, fmt.Errorf("%s list is required", name)
	}
	names := make([]string, 0, list.Len())
	for i := range list.Len() {
		name, ok := starlark.AsString(list.Index(i))
		if !ok {
			return nil, fmt.Errorf("%s[%d] is not a string", name, i)
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"fmt"

	"go.starlark.net/starlark"
)

// Common Starlark utilities.

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

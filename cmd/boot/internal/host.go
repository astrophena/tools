// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// host implements the host() builtin.
//
// Recipes often need a small amount of runtime metadata before registering
// tasks. Exposing it through one struct keeps those top-level decisions
// declarative and avoids shelling out from Starlark just to learn the hostname,
// recipe root, home directory, or privilege mode.
func (e *Engine) host(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(b.Name(), args, kwargs); err != nil {
		return nil, err
	}
	if e.Runtime == nil {
		return nil, fmt.Errorf("%s: runtime is not configured", b.Name())
	}
	hostname, err := e.Runtime.Hostname()
	if err != nil {
		return nil, err
	}
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"home":        starlark.String(e.Runtime.Home),
		"hostname":    starlark.String(hostname),
		"interactive": starlark.Bool(e.Runtime.Interactive),
		"needs_sudo":  starlark.Bool(e.Runtime.NeedsSudo()),
		"root":        starlark.String(e.Runtime.Root),
	}), nil
}

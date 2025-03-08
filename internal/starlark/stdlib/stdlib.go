// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package stdlib contains the [Starlark] standard library code.
//
// [Starlark]: https://starlark-lang.org
package stdlib

import (
	"embed"

	"go.astrophena.name/tools/internal/starlark/interpreter"
)

//go:embed *.star
var fs embed.FS

// Loader returns a loader that loads files from the standard library.
func Loader() interpreter.Loader { return interpreter.FSLoader(fs) }

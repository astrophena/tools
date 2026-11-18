// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package llm exposes text generation and token usage to Starlark.
package llm

import _ "embed"

//go:embed doc.md
var doc string

// Documentation returns the Starlark module's Markdown help.
func Documentation() string { return doc }

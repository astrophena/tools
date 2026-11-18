// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package kvcache exposes a key-value cache to Starlark.
package kvcache

import _ "embed"

//go:embed doc.md
var doc string

// Documentation returns the Starlark module's Markdown help.
func Documentation() string { return doc }

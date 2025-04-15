// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Gox is like 'go run', but caches compiled binaries based on hash of the program.

It's suitable for single-file Go programs that depend only on the standard
library.

# Usage

	$ gox <program.go> [arguments...]
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }

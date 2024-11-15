// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Renamer renames files in a specified directory sequentially, starting from a
given number.

# Usage

	$ renamer [flags...] <dir>

It sorts the files based on name, time, size, or type before renaming. If a
file with the new name already exists, it skips the file and continues to the
next.
*/
package main

import (
	_ "embed"

	"go.astrophena.name/tools/internal/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }

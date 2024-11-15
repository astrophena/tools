// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Txtar is a tool for creating and extracting txtar archives.

# Usage

To create a txtar archive:

	$ txtar -c <archive> <directory>

This will create a txtar archive named <archive> containing all the files and
directories from the specified <directory>.

To extract a txtar archive:

	$ txtar <archive> <directory>

This will extract the contents of the txtar archive named <archive> into the
specified directory.
*/
package main

import (
	_ "embed"

	"go.astrophena.name/tools/internal/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }

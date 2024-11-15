// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Mdserve serves Markdown files from a directory.

# Usage

	$ mdserve [flags...] [dir]
*/
package main

import (
	_ "embed"

	"go.astrophena.name/tools/internal/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }

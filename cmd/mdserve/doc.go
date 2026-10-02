// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Mdserve previews Markdown files in a browser, with a table of contents and
click-to-enlarge images. Other files are served unchanged.

# Usage

	$ mdserve [flags...] [dir]
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }

// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Kp reads entries from a KeePass database.

# Usage

	$ kp <file> <entry>
	$ kp -l <file>

Kp prints passwords by default. Use -f to select a Go template over the entry
fields at https://pkg.go.dev/github.com/tobischo/gokeepasslib/v3#Entry.

Set KP_PASSWORD to supply the database password. Otherwise, kp prompts for it.
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }

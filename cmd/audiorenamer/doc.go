// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Audiorenamer renames audio files using their tags.

# Usage

	$ audiorenamer [flags...] <dir>

Use -format to set the Go filename template. It takes metadata fields from
https://pkg.go.dev/github.com/dhowden/tag#Metadata. The track function returns
the track number. Slashes are stripped from the result.
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }

// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Starlet runs Telegram bots written in Starlark. It loads bot.star from a GitHub
Gist and calls handle(update) for incoming Telegram webhooks.

# Usage

	$ starlet [flags...]

# Starlark Environment

See https://bot.astrophena.name/env.
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }

// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Kp retrieves passwords and other information from KeePass database files.

# Usage

To retrieve a password for a specific entry:

	$ kp <file> <entry>

To list all entries in the database:

	$ kp -l <file>

By default, kp retrieves the password associated with an entry. You can
customize the output format using the -f flag, which accepts a Go template
string:

	$ kp -f "{{ .GetUsername }}:{{ .GetPassword }}" <file> <entry>

The available fields for the template are documented at:
https://pkg.go.dev/github.com/tobischo/gokeepasslib/v3#Entry.

The password for the KeePass database can be provided via the KP_PASSWORD
environment variable. If the environment variable is not set, kp will
prompt for the password interactively.
*/
package main

import (
	_ "embed"

	"go.astrophena.name/tools/internal/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }

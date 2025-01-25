// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:build linux && !android

/*
Dungeon sandboxes programs so they can't fulfill their deep dark fantasies on
unsuspecting users. It uses Landlock LSM to restrict program's access to the file
system and network.

# Usage

	$ dungeon -config <config file path> <program> [program args...]

The <program> is a path to executable binary and [program args...] are arguments
passed to the program.

# Config

Dungeon uses JSON config file to describe allowed program's access.

Example:

	{
	  "fs": {
	    "ro_dirs": ["/usr/bin", "/usr/lib"],
	    "rw_dirs": ["/tmp"],
	    "ro_files": ["/etc/passwd"],
	    "rw_files": []
	  },
	  "network": {
	    "allowed_ports": [80, 443],
	    "allowed_bindings": [8080]
	  }
	}

The config contains two main sections: fs and network.

The fs section describes allowed access to the file system.

ro_dirs is an array of paths to directories that the sandboxed
program can only read from.

rw_dirs is an array of paths to directories that the sandboxed
program can read from and write to.

ro_files is an array of paths to files that the sandboxed program
can only read from.

rw_files is an array of paths to files that the sandboxed program
can read from and write to.

The network section describes allowed access to the network.

allowed_ports is an array of TCP ports that the sandboxed program
is allowed to connect to.

allowed_bindings is an array of TCP ports that the sandboxed program
is allowed to bind to.

# Notes

Dungeon only works on Linux with Landlock LSM support.
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }

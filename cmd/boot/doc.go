// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Boot runs Starlark recipes to set up a host. Recipes register tasks that use
built-in modules to check or change files, packages, repositories, and services.
Actions skip work when the host already matches the recipe.

# Usage

	$ boot [flags] <list|plan|check|apply>

List shows tasks. Plan checks actions and reports proposed changes without
applying them; check is an alias for plan. Apply makes the changes. Flag
descriptions are available through -help.

# Recipes

Boot loads BOOT.star from the current directory by default. Use -C to select a
directory or -f to select another entrypoint. Register tasks at the top level;
use module functions inside tasks to create actions:

	def dotfiles():
	    fs.symlink("bash/rc", "~/.bashrc")

	task(id="dotfiles", name="Link dotfiles", run=dotfiles)

Tasks can declare dependencies and tags. Use -only, -skip, and -tag to select
tasks. See cmd/boot/modules.md for built-in functions and modules.

Use consent.require in a task to ask before applying later actions. Apply holds
a lock so two runs of the same recipe cannot overlap.

# Configuration

Boot reads $XDG_CONFIG_HOME/boot/config.star, or ~/.config/boot/config.star if
XDG_CONFIG_HOME is unset. The file may call boot.configure to set workspace,
entry, concurrency, fail_fast, verbose, and json defaults. Flags override these
defaults.
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }

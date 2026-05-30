// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Boot applies a Starlark recipe to bring a development environment into the
state described by that recipe. It is intended for personal workstation and
shell-environment bootstrap tasks: package installation, dotfile links, Git
checkouts, generated keys, timers, and other small host maintenance actions.

Boot recipes register named tasks. When a task runs, it emits idempotent
actions through built-in modules such as fs, git, pkg, go, systemd, shell, and
ssh. Boot then checks each action and either skips it, reports that it would
change the host, or applies it.

Boot is deliberately smaller than Ansible. Recipes are ordinary Starlark files,
module APIs are narrow Go wrappers, and the plan/apply split is the main safety
mechanism.

# Usage

	$ boot [flags...] <command>

Where <command> is one of the following commands:

	list
		List selected tasks without running them.

	plan
		Evaluate selected tasks and print the actions they would take without
		changing the host.

	check
		Alias for plan intended for validation scripts.

	apply
		Evaluate selected tasks and apply their actions.

# Flags

	-C dir
		Run as if boot was started in dir. Defaults to the current directory.

	-f file
		Starlark recipe entrypoint. Defaults to BOOT.star.

	-dry-run
		Alias for plan.

	-json
		Print machine-readable JSON output.

	-fail-fast
		Stop on the first failed task.

	-only task
		Run only the specified task ID. May be repeated.

	-skip task
		Skip the specified task ID. May be repeated.

	-tag tag
		Run tasks with the specified tag. May be repeated.

	-j concurrency
		Number of tasks to run in parallel. Defaults to 1.

# Recipes

Recipes are Starlark files loaded from the directory selected by -C. The default
entrypoint is BOOT.star. Top-level code should detect the current machine and
register tasks; task functions should emit actions and avoid doing direct host
mutation themselves:

	# vim: ft=starlark shiftwidth=4

	def dotfiles():
	    fs.dir("~/local/data/bash")
	    fs.symlink("bash/rc", "~/.bashrc")

	def supported():
	    if env.get("HOME") == "":
	        fail("HOME is required")

	task(
	    id="dotfiles",
	    name="Link dotfiles",
	    tags=["filesystem"],
	    run=dotfiles,
	)

Tasks may declare tags, dependencies, whether failures should be continuable,
and whether sudo should be prepared before apply. Select tasks with -only,
-skip, and -tag.

For built-in module documentation, see cmd/boot/modules.md.

# Safety

The plan command runs action checks but does not apply changes. Actions should
therefore make dry-run checks cheap and side-effect free. Use consent.require
inside a task when an apply needs an explicit user acknowledgement before later
actions in that task proceed.

For system tasks, set requires_sudo=True on the task so Boot can authenticate
once before apply. Individual modules still decide when sudo is necessary.

The apply command holds a per-recipe advisory lock so two Boot runs cannot race
the same package manager or filesystem actions.

# Environment Variables

Boot reads the following environment variables:

	HOME
		Used when expanding paths that begin with ~/.

	BOOT_PACKAGE_MANAGER
		Default package manager for pkg.install when pkg.configure is not used.

	NO_COLOR
		Disable colored output.

	CI
		Disable interactive prompts when set to true.
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }

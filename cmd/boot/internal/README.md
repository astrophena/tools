# Hacking on Boot

`cmd/boot` handles flags and commands. This package loads recipes, runs tasks,
and provides the shared API for built-in modules.

## Runtime

`cmd/boot/main.go` creates an `Engine` with a `Runtime`, a recipe entrypoint
(usually `BOOT.star`), and the available modules. `Engine.Load` evaluates the
recipe with `task`, `fail`, `host`, and the modules as Starlark globals.
Top-level code should register tasks; task functions create actions through
modules.

When a task runs, the engine stores it on the Starlark thread. Module functions
call `RequireTask` before `AddAction` to attach an action to that task. Each
action has a `Summary` and an `Apply(ctx, dryRun)` function. The function checks
the host in dry-run mode and makes the change during apply. It returns:

- `ResultSkip` if the host already matches;
- `ResultChange` if it changed the host or would change it;
- `ResultStop` to stop later actions during apply, as `consent.require` does.

Use `Warn` to add a non-fatal message to the run's final report.

## Modules

A module implements `Name() string` and `Members(*Runtime) starlark.StringDict`.
Keep its Starlark functions focused on recipe operations. Functions that emit
actions should:

- Call `RequireTask` and parse arguments with `starlark.UnpackArgs`.
- Use `Runtime` path and environment helpers for recipe inputs and host targets.
- Add actions without changing the host during recipe evaluation.
- Make dry-run checks read-only and report command failures with useful output.

The shared helpers in this package cover command execution, warnings, file
modes, and output formatting. See existing modules for examples.

## Testing

Put tests next to each module. Use `testutil.NewTask` and `EmitOne` to call a
Starlark function and inspect its action. Test dry-run and apply when the action
can write. `testutil.Commands` installs fake commands in a temporary `PATH`.

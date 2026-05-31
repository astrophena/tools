# Hacking on Boot

Boot is a small Starlark runtime for host setup recipes. The public binary lives
in `cmd/boot`; this package contains the task engine, runtime state, and module
contracts used by the built-in modules.

## Runtime Model

`main.go` builds an `internal.Engine` with:

- a `Runtime`, which stores recipe root, home directory, environment access,
  stdio, and whether the current run is interactive;
- an entrypoint, normally `BOOT.star`;
- a list of modules, each exposed as a Starlark module.

`Engine.Load` executes the entrypoint with predeclared globals:

- `task(...)` registers a task;
- `fail(message)` stops recipe evaluation;
- each Go module appears under its module name, such as `fs` or `pkg`.

Top-level Starlark should only register tasks and choose machine profiles. Host
mutation belongs in task functions through module actions.

## Tasks and Actions

A task is a named Starlark callable. When the engine runs a task, it attaches
the task to the Starlark thread with `SetTask`. Module functions validate
`InTask(thread)` and call `AddAction(thread, Action{...})`.

An `Action` has:

- `Summary`, printed in plans, verbose applies, and failures;
- `Apply(ctx, dryRun)`, which checks or applies one idempotent operation.

Action results are:

- `ResultSkip`: the host already matched the requested state;
- `ResultChange`: the action changed the host or would change it in dry-run;
- `ResultWarn`: the action found a non-fatal issue;
- `ResultStop`: stop the remaining actions in this task without failing.

Use `ResultStop` only for gating actions such as `consent.require`; normal
idempotency should use skip/change.

## Writing Modules

Modules implement:

```go
type Module interface {
    Name() string
    Members(*Runtime) starlark.StringDict
}
```

Keep module APIs narrow and recipe-oriented. Prefer one clear Starlark function
that emits one idempotent action over a general-purpose command wrapper.

Module function checklist:

- Require task context for functions that emit actions.
- Parse arguments with `starlark.UnpackArgs`.
- Resolve recipe inputs with `Runtime.ResolveSource`.
- Resolve host targets with `Runtime.ResolveTarget`.
- Use `Runtime.ExpandHome` or `Runtime.Hostname` rather than duplicating that
  logic.
- Do not mutate the host while registering actions.
- In dry-run, perform enough checks to decide skip/change but do not write.
- Include command output in returned errors; use `boot.CommandError` when it
  fits.
- Keep successful JSON or textual output minimal; noisy reporting belongs in
  explicit check modules.

Avoid external dependencies for modules unless the standard library would make
the code fragile or much larger.

## Debugging Modules

Start with a focused recipe and task selection:

```sh
go run ./cmd/boot -C ~/code/prefs -only setup_packages plan
go run ./cmd/boot -C ~/code/prefs -only setup_packages -verbose apply
```

Use `plan` to verify the action list and idempotency checks. Use `-verbose
apply` when you need per-action skip/change output. For task filtering bugs,
`boot list`, `-only`, `-skip`, and `-tag` exercise the selection path without
running actions.

Use `--json` when another program needs stable output. JSON runs intentionally
use a simpler sequential execution path so action results are ordered and easy
to consume.

When debugging command execution, prefer fake commands in a temporary `PATH`
inside tests. See the `packages`, `systemd`, and `rescue` tests for examples.

## Testing

Put module tests next to the module. Use `testutil.TaskThread` to create a task
and Starlark thread, call the module function, then run the emitted action.

Typical shape:

```go
task, thread := testutil.TaskThread("test")
m := &impl{rt: &boot.Runtime{Root: root, Home: home, Getenv: os.Getenv}}
_, err := m.someFunction(thread, starlark.NewBuiltin("module.func", m.someFunction), nil, kwargs)
if err != nil {
    t.Fatal(err)
}
got, err := task.Actions[0].Apply(context.Background(), true)
```

Tests should cover both dry-run and apply behavior when a function can write.
For command-based modules, create small executable shell scripts with
`testutil.WriteCommand` and prepend their directory to `PATH`.

Before submitting changes, run from the repository root:

```sh
go tool pre-commit
```

## Recipe Compatibility

Boot currently targets the prefs setup recipes, so compatibility with the old
`automation/setup` behavior matters. Be careful around:

- package updates, which may need explicit consent;
- maintenance checks, which should warn unless the recipe intentionally wants a
  hard failure;
- environment loading before tools that rely on `GOBIN`, `GOPATH`, or XDG
  variables;
- dry-run behavior for system modules.

When replacing old setup behavior, read the old Python task and port the
observable behavior first. Then simplify only when the new behavior is
intentional and documented.

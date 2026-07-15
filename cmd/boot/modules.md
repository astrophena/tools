# Starlark Environment

These built-in functions and modules are available in the Starlark environment.

## Built-ins

### `fail(message)`

Stop recipe or task execution with `message`.

### `host()`

Return host/runtime metadata as a struct with `hostname`, `home`, `root`,
`needs_sudo`, and `interactive` fields. This is intended for top-level recipe
branching before tasks are registered.

### `task(..., when = True)`

Register a task. When `when` is false, the task is not registered; this is a
small replacement for wrapping conditional machine tasks in extra Starlark
control flow.

## `consent`

### `consent.require(message, default = False)`

Ask the user for confirmation before continuing the current task. In dry-run
mode this is reported as a planned change. In non-interactive mode, or when the
answer is no, remaining actions in the task are skipped.

## `env`

### `env.command_exists(name)`

Return whether `name` can be found in `PATH`.

### `env.get(key, default = "")`

Return the value of the environment variable `key`. If the variable is not set
or is empty, return `default`.

### `env.hostname()`

Return the system host name.

### `env.load_dir(path)`

Load shell-style `*.conf` environment files from `path` in lexical order into
the current process. Later commands inherit these variables.

## `fs`

### `fs.dir(path)`

Ensure `path` exists as a directory.

### `fs.symlink(source, target, backup = False)`

Ensure `target` is a symbolic link to `source`. Relative sources are resolved
against the recipe root; paths beginning with `~/` use `HOME`.

### `fs.file(path, content, mode = 0o644)`

Ensure `path` exists with the given content and mode.

### `fs.template(path, template, values, mode = 0o644)`

Render `template` to `path`, replacing `{{name}}` placeholders with values from
the `values` dictionary.

### `fs.sha256(path)`

Return the SHA-256 digest of `path`.

### `fs.newer(source, target)`

Return whether `source` has a newer modification time than `target`, or whether
`target` does not exist.

### `fs.remove(path)`

Ensure `path` does not exist (removes it if it does).

### `fs.chmod(path, mode)`

Ensure `path` has the given permission mode.

### `fs.sync_tree(source, target, owner = "", group = "", sudo = False, only_if_exists = False)`

Synchronize a directory tree with `rsync -a`. If `owner` or `group` is set,
pass `--chown`. If `only_if_exists` is true, skip when `source` is missing.

## `fetch`

### `fetch.file(url, path, mode = 0o644, checksum = "")`

Ensure `path` exists with the contents downloaded from `url`. If `checksum` is
set, it must be a SHA-256 hex digest, optionally prefixed with `sha256:`.

## `flatpak`

### `flatpak.update()`

Update Flatpak applications when updates are available. Skips when `flatpak`
is not installed or no updates are pending.

## `pkg`

### `pkg.configure(manager)`

Select the package manager. Supported values are `apt` and `pacman`. If this
is not set, boot checks `BOOT_PACKAGE_MANAGER` and then detects `pacman` or `apt`
from the host environment.

### `pkg.install(packages)`

Install missing packages. The package database is checked before invoking
the package manager, so no-op runs are fast.

### `pkg.update()`

Update the package manager's database and upgrade installed packages. With
`pacman`, boot first checks for pending updates using a separate database under
`$XDG_CACHE_HOME/boot/pacman` (or `~/.cache/boot/pacman`) and skips when the
system is already current. If a pending package owns files under
`/usr/lib/modules`, planning warns that the update will require a reboot and a
successful apply warns that the machine needs rebooting.

### `pkg.check_explicit_packages(packages)`

Warn when explicitly installed native packages are not listed in `packages`, for
package managers that can report manual/native package state.

### `pkg.manager()`

Return the selected or detected package manager name.

## `pacman`

### `pacman.check_orphans()`

Warn when orphaned packages are installed.

### `pacman.check_explicit_packages(packages)`

Warn when explicitly installed native packages are not listed in `packages`.

### `pacman.check_pacnew(managed_etc)`

Warn when `.pacnew` files exist under `/etc`, grouping files managed by
`managed_etc` separately from unmanaged files and showing short diffs.

## `program`

### `program.update(argv)`

Update a program through Boot's check protocol. `argv` is the command that
applies the update and is executed directly without a shell. To check whether
an update is needed, Boot appends `-check`; the program must print only `true`
or `false` to standard output. When the result is `true`, planning reports a
change and applying runs the original command.

The `-check` invocation must be read-only. Diagnostics may be written to
standard error; a failed check, failed update, or any other standard output is
reported as an action failure.

## `rescue`

### `rescue.update(source, esp_dir = "/efi/EFI/Linux", keep = 3)`

Build and install an Arch rescue image from `source` when the current ESP image
is not from the current month. Signed images are installed with `sbctl`; old ESP
images are pruned after `keep` entries.

## `shell`

### `shell.run(command, creates = "", only_if = "", cwd = "", sudo = False)`

Run a shell command. If `creates` is set, the command is skipped if the
path exists. If `only_if` is set, the command is run only if the `only_if`
command succeeds (returns 0). If `cwd` is set, the command runs in that directory.
If `sudo` is true, the command will be executed with elevated privileges via `sudo`.

### `shell.output(command, cwd = "", sudo = False)`

Run a shell command and return its standard output as a string. This is executed
immediately during recipe evaluation, not during the apply phase. If `sudo` is true, 
it executes with elevated privileges.

## `systemd`

### `systemd.system_unit(name, enabled = False, started = False, daemon_reload = False)`

Ensure a system unit is enabled and/or started, optionally reloading system units
first when systemd reports that a daemon reload is needed.

### `systemd.user_unit(name, enabled = False, started = False, daemon_reload = False)`

Ensure a user unit is enabled and/or started, optionally reloading user units
first when systemd reports that a daemon reload is needed.

## `git`

### `git.clone(url, dest, revision = "")`

Ensure `url` is cloned to `dest`. If `revision` is set, check out that concrete
revision as a detached HEAD. Relative `dest` is resolved against the recipe root;
paths beginning with `~/` use `HOME`.

### `git.pull(dest)`

Ensure the git repository at `dest` is up-to-date. It skips if the repository is dirty or has no upstream tracking branch.

### `git.sync(url, dest, revision = "")`

Ensure `url` is cloned to `dest`, or fetch and fast-forward pull the existing
repository when it is clean and has an upstream tracking branch. If `revision` is
set, fetch and check out that concrete revision as a detached HEAD instead of
fast-forwarding the upstream branch.

## `go`

### `go.install(packages, cwd = "", ldflags = "-s -w -buildid=", trimpath = True)`

Run `go install` separately for each package in the given list. Existing
binaries are skipped when their embedded build information already matches the
requested version. For `@latest`, boot compares the installed module version
with the latest version reported by `go list -m`.

### `go.install_local(package, cwd, fallback_latest = True, ldflags = "-s -w -buildid=", trimpath = True)`

Run `go install` for `package` from `cwd` when that checkout exists. If
`fallback_latest` is true and `cwd` does not exist, install `package@latest`.
Local binaries are skipped when their embedded VCS revision matches the checkout
HEAD and the checkout is clean.

## `ssh`

### `ssh.key(path, type = "ed25519", comment = "", passphrase = "")`

Ensure an SSH key exists at `path`, generating it with `ssh-keygen` when missing.
If `comment` is empty, the host name is used.

# Starlark API

Recipes register tasks at the top level. Most setup functions add actions
inside a task's `run` function. Planning checks those actions without applying
them. Query and configuration functions run when called.

Relative paths use the recipe directory. Paths beginning with `~/` use the home
directory.

## Built-ins

- `fail(message)` stops recipe or task evaluation with an error.
- `host()` returns `hostname`, `home`, `root`, `needs_sudo`, and `interactive`
  fields for choosing tasks at the top level.
- `struct(...)` groups named values into a Starlark struct.
- `task(id, name, run, tags=[], depends_on=[], continue_on_error=False, requires_sudo=False, when=True)`
  registers a task. `when=False` leaves it out. `depends_on` names prerequisite
  task IDs; `requires_sudo` prepares sudo before apply; `continue_on_error`
  prevents its failures from stopping a fail-fast run.

## `consent`

- `consent.require(message, default=False)` asks before later actions in the
  task run. Planning reports the prompt; a refusal or non-interactive apply
  skips the remaining actions. `default` controls an empty answer.

## `env`

- `env.command_exists(name)` reports whether `name` is in `PATH`.
- `env.get(key, default="")` returns an environment value, using `default` if
  it is unset or empty.
- `env.hostname()` returns the host name.
- `env.load_dir(path)` immediately loads `*.conf` files in lexical order. Later
  commands inherit their variables.

## `fs`

- `fs.dir(path)` ensures a directory exists.
- `fs.symlink(source, target, backup=False)` links `target` to `source`. With
  `backup=True`, it renames an existing target before replacing it.
- `fs.file(path, content, mode=0o644)` writes a file with the given content and
  mode.
- `fs.template(path, template, values, mode=0o644)` writes a file after
  replacing `{{name}}` placeholders from `values`.
- `fs.sha256(path)` returns a file's SHA-256 digest.
- `fs.newer(source, target)` reports whether `source` is newer or `target` is
  missing.
- `fs.remove(path)` removes a path if it exists.
- `fs.chmod(path, mode)` sets a path's permissions.
- `fs.sync_tree(source, target, owner="", group="", sudo=False, only_if_exists=False)`
  syncs a tree with `rsync`. `owner` and `group` set ownership;
  `only_if_exists=True` skips a missing source.

## `fetch`

- `fetch.file(url, path, mode=0o644, checksum="")` downloads a missing file.
  With a checksum, it also replaces a file whose contents differ. Without one,
  an existing file with the requested mode is considered current. `checksum`
  is a SHA-256 hex digest, with an optional `sha256:` prefix.

## `flatpak`

- `flatpak.update()` updates installed applications when updates are available.
  It skips hosts without Flatpak.

## `pkg`

- `pkg.configure(manager)` selects `apt` or `pacman`. Otherwise Boot checks
  `BOOT_PACKAGE_MANAGER`, then detects an installed manager.
- `pkg.manager()` returns the selected or detected manager name.
- `pkg.install(packages)` installs missing packages.
- `pkg.update()` upgrades available packages. A pacman plan checks the local
  package database; apply refreshes a separate database before upgrading and
  warns when an update requires a reboot.
- `pkg.check_explicit_packages(packages)` warns about manually installed
  packages missing from the list, when the manager supports that check.

## `pacman`

- `pacman.check_orphans()` warns about orphaned packages.
- `pacman.check_explicit_packages(packages)` warns about explicitly installed
  native packages missing from the list.
- `pacman.check_pacnew(managed_etc)` warns about `.pacnew` files under `/etc`,
  grouping files found in `managed_etc` separately and showing diffs.

## `program`

- `program.update(argv)` runs an updater without a shell. Boot first appends
  `-check` to `argv`; the command must print only `true` or `false`. A `true`
  result plans an update and runs the original command during apply. The check
  command must not change the host.

## `rescue`

- `rescue.update(source, esp_dir="/efi/EFI/Linux", keep=3)` builds and signs an
  Arch rescue image when the installed image is from an earlier month. It
  removes old images beyond `keep`.

## `systemd`

- `systemd.system_unit(name, enabled=False, started=False, daemon_reload=False)`
  ensures a system unit is enabled and/or started. `daemon_reload=True` reloads
  systemd first when needed.
- `systemd.user_unit(name, enabled=False, started=False, daemon_reload=False)`
  does the same for a user unit.

## `git`

- `git.clone(url, dest, revision="")` clones a repository if missing. With
  `revision`, it checks out that revision as a detached HEAD.
- `git.pull(dest)` updates a clean repository with an upstream branch; it skips
  dirty repositories and those without an upstream.
- `git.sync(url, dest, revision="")` clones or fast-forwards a clean
  repository. With `revision`, it fetches and checks out that revision instead.

## `go`

- `go.install(packages, cwd="", ldflags="-s -w -buildid=", trimpath=True)`
  installs each package separately, skipping binaries already at the requested
  version. For `@latest`, it checks the latest module version.
- `go.install_local(package, cwd, fallback_latest=True, ldflags="-s -w -buildid=", trimpath=True)`
  installs from a local checkout when it exists. If it is missing and
  `fallback_latest=True`, it installs `package@latest`. A clean checkout whose
  revision matches the installed binary is skipped.

## `ssh`

- `ssh.key(path, type="ed25519", comment="", passphrase="")` generates a key
  with `ssh-keygen` if it is missing. An empty comment uses the host name.

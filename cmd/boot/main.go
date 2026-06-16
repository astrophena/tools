// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.astrophena.name/base/cli"
	boot "go.astrophena.name/tools/cmd/boot/internal"
	bootconsent "go.astrophena.name/tools/cmd/boot/internal/consent"
	bootenv "go.astrophena.name/tools/cmd/boot/internal/env"
	bootfetch "go.astrophena.name/tools/cmd/boot/internal/fetch"
	bootflatpak "go.astrophena.name/tools/cmd/boot/internal/flatpak"
	bootfs "go.astrophena.name/tools/cmd/boot/internal/fs"
	bootgit "go.astrophena.name/tools/cmd/boot/internal/git"
	bootgo "go.astrophena.name/tools/cmd/boot/internal/golang"
	bootpkg "go.astrophena.name/tools/cmd/boot/internal/packages"
	bootpacman "go.astrophena.name/tools/cmd/boot/internal/pacman"
	bootrescue "go.astrophena.name/tools/cmd/boot/internal/rescue"
	bootshell "go.astrophena.name/tools/cmd/boot/internal/shell"
	bootssh "go.astrophena.name/tools/cmd/boot/internal/ssh"
	bootsystemd "go.astrophena.name/tools/cmd/boot/internal/systemd"
	"go.astrophena.name/tools/internal/filelock"

	"golang.org/x/term"
)

const defaultEntry = "BOOT.star"

func main() { cli.Main(new(app)) }

type app struct {
	root        flagValue[string]
	entry       flagValue[string]
	failFast    flagValue[bool]
	only        multiFlag
	skip        multiFlag
	tags        multiFlag
	dryRun      bool
	verbose     flagValue[bool]
	json        flagValue[bool]
	concurrency flagValue[int]
}

func (a *app) Flags(fs *flag.FlagSet) {
	a.root = newFlagValue(".", parseString)
	a.entry = newFlagValue(defaultEntry, parseString)
	a.failFast = newBoolFlagValue(false)
	a.verbose = newBoolFlagValue(false)
	a.json = newBoolFlagValue(false)
	a.concurrency = newFlagValue(1, parsePositiveInt)
	fs.Var(&a.root, "C", "Run as if boot was started in `dir`.")
	fs.Var(&a.entry, "f", "Recipe entrypoint `file`.")
	fs.Var(&a.failFast, "fail-fast", "Stop on the first failed task.")
	fs.BoolVar(&a.dryRun, "dry-run", false, "Alias for plan: print actions without applying changes.")
	fs.Var(&a.verbose, "verbose", "Print detailed action output when applying changes.")
	fs.Var(&a.json, "json", "Print machine-readable JSON output.")
	fs.Var(&a.only, "only", "Run only the specified task ID. May be repeated.")
	fs.Var(&a.skip, "skip", "Skip the specified task ID. May be repeated.")
	fs.Var(&a.tags, "tag", "Run tasks with the specified tag. May be repeated.")
	fs.Var(&a.concurrency, "j", "Number of tasks and eligible actions to run in parallel.")
}

func (a *app) Run(ctx context.Context) error {
	env := cli.GetEnv(ctx)
	if len(env.Args) != 1 {
		return fmt.Errorf("%w: command is required\nusage: boot [flags...] <list|plan|apply>\nrun 'boot -help' for details", cli.ErrInvalidArgs)
	}

	command := env.Args[0]
	if a.dryRun {
		command = "plan"
	}

	cfg, err := loadConfig(ctx, env)
	if err != nil {
		return fmt.Errorf("%w: %v", cli.ErrInvalidArgs, err)
	}
	a.applyConfig(env, cfg)

	engine, err := a.engine(env)
	if err != nil {
		return err
	}
	if err := engine.Load(ctx); err != nil {
		return fmt.Errorf("%w: %v", cli.ErrInvalidArgs, err)
	}

	selection := boot.Selection{
		Only: a.only,
		Skip: a.skip,
		Tags: a.tags,
	}
	switch command {
	case "list":
		return engine.List(env.Stdout, selection)
	case "plan", "check":
		return engine.Run(ctx, env.Stdout, selection, boot.RunOptions{
			DryRun:      true,
			FailFast:    a.failFast.value,
			Interactive: interactive(env),
			Concurrency: a.concurrency.value,
			Color:       env.Getenv("NO_COLOR") == "",
			Verbose:     a.verbose.value,
			JSON:        a.json.value,
		})
	case "apply":
		lock, err := acquireLock(rootLockPath(engine.Runtime.Root))
		if err != nil {
			return err
		}
		defer lock.Release()
		return engine.Run(ctx, env.Stdout, selection, boot.RunOptions{
			FailFast:    a.failFast.value,
			Interactive: interactive(env),
			Concurrency: a.concurrency.value,
			Color:       env.Getenv("NO_COLOR") == "",
			Verbose:     a.verbose.value,
			JSON:        a.json.value,
		})
	default:
		return fmt.Errorf("%w: no such command %q", cli.ErrInvalidArgs, command)
	}
}

func rootLockPath(root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(os.TempDir(), "boot-"+hex.EncodeToString(sum[:8])+".lock")
}

func acquireLock(path string) (filelock.Lock, error) {
	lock, err := filelock.Acquire(path, fmt.Sprintf("pid=%d\n", os.Getpid()))
	if err == nil {
		return lock, nil
	}
	if errors.Is(err, filelock.ErrAlreadyLocked) {
		return nil, fmt.Errorf("another boot apply is already running for this recipe")
	}
	return nil, err
}

func interactive(env *cli.Env) bool {
	if env.Getenv("CI") == "true" {
		return false
	}
	in, inOK := env.Stdin.(*os.File)
	out, outOK := env.Stdout.(*os.File)
	return inOK && outOK && term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd()))
}

func (a *app) applyConfig(env *cli.Env, cfg configOptions) {
	if cfg.workspace.set && !a.root.set {
		a.root.value = expandHome(env, cfg.workspace.value)
	}
	if cfg.entry.set && !a.entry.set {
		a.entry.value = cfg.entry.value
	}
	if cfg.concurrency.set && !a.concurrency.set {
		a.concurrency.value = cfg.concurrency.value
	}
	if cfg.failFast.set && !a.failFast.set {
		a.failFast.value = cfg.failFast.value
	}
	if cfg.verbose.set && !a.verbose.set {
		a.verbose.value = cfg.verbose.value
	}
	if cfg.json.set && !a.json.set {
		a.json.value = cfg.json.value
	}
}

func (a *app) engine(env *cli.Env) (*boot.Engine, error) {
	root, err := filepath.Abs(a.root.value)
	if err != nil {
		return nil, err
	}
	home := env.Getenv("HOME")
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	rt := &boot.Runtime{
		Root:   root,
		Home:   home,
		Getenv: env.Getenv,
		Stdin:  env.Stdin,
		Stdout: env.Stdout,
	}
	return &boot.Engine{
		Runtime: rt,
		Entry:   a.entry.value,
		Modules: []boot.Module{
			bootconsent.Module(),
			bootenv.Module(),
			bootfetch.Module(),
			bootflatpak.Module(),
			bootfs.Module(),
			bootpkg.Module(),
			bootpacman.Module(),
			bootshell.Module(),
			bootgit.Module(),
			bootgo.Module(),
			bootrescue.Module(),
			bootssh.Module(),
			bootsystemd.Module(),
		},
	}, nil
}

type multiFlag []string

func (f *multiFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *multiFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

type flagValue[T any] struct {
	value  T
	set    bool
	isBool bool
	parse  func(string) (T, error)
}

func newFlagValue[T any](value T, parse func(string) (T, error)) flagValue[T] {
	return flagValue[T]{value: value, parse: parse}
}

func newBoolFlagValue(value bool) flagValue[bool] {
	return flagValue[bool]{value: value, isBool: true, parse: strconv.ParseBool}
}

func (f *flagValue[T]) String() string {
	return fmt.Sprint(f.value)
}

func (f *flagValue[T]) Set(value string) error {
	parsed, err := f.parse(value)
	if err != nil {
		return err
	}
	f.value = parsed
	f.set = true
	return nil
}

func (f *flagValue[T]) IsBoolFlag() bool {
	return f.isBool
}

func parseString(value string) (string, error) {
	return value, nil
}

func parsePositiveInt(value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	if n < 1 {
		return 0, fmt.Errorf("must be at least 1")
	}
	return n, nil
}

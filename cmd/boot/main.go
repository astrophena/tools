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
	root        string
	entry       string
	failFast    bool
	only        multiFlag
	skip        multiFlag
	tags        multiFlag
	dryRun      bool
	verbose     bool
	json        bool
	concurrency int
}

func (a *app) Flags(fs *flag.FlagSet) {
	fs.StringVar(&a.root, "C", ".", "Run as if boot was started in `dir`.")
	fs.StringVar(&a.entry, "f", defaultEntry, "Recipe entrypoint `file`.")
	fs.BoolVar(&a.failFast, "fail-fast", false, "Stop on the first failed task.")
	fs.BoolVar(&a.dryRun, "dry-run", false, "Alias for plan: print actions without applying changes.")
	fs.BoolVar(&a.verbose, "verbose", false, "Print detailed action output when applying changes.")
	fs.BoolVar(&a.json, "json", false, "Print machine-readable JSON output.")
	fs.Var(&a.only, "only", "Run only the specified task ID. May be repeated.")
	fs.Var(&a.skip, "skip", "Skip the specified task ID. May be repeated.")
	fs.Var(&a.tags, "tag", "Run tasks with the specified tag. May be repeated.")
	fs.IntVar(&a.concurrency, "j", 1, "Number of tasks to run in parallel.")
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
			FailFast:    a.failFast,
			Interactive: interactive(env),
			Concurrency: a.concurrency,
			Color:       env.Getenv("NO_COLOR") == "",
			Verbose:     a.verbose,
			JSON:        a.json,
		})
	case "apply":
		lock, err := acquireLock(rootLockPath(engine.Runtime.Root))
		if err != nil {
			return err
		}
		defer lock.Release()
		return engine.Run(ctx, env.Stdout, selection, boot.RunOptions{
			FailFast:    a.failFast,
			Interactive: interactive(env),
			Concurrency: a.concurrency,
			Color:       env.Getenv("NO_COLOR") == "",
			Verbose:     a.verbose,
			JSON:        a.json,
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

func (a *app) engine(env *cli.Env) (*boot.Engine, error) {
	root, err := filepath.Abs(a.root)
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
		Entry:   a.entry,
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

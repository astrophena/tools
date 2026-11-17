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
	"go.astrophena.name/base/filelock"
	boot "go.astrophena.name/tools/cmd/boot/internal"
	bootconsent "go.astrophena.name/tools/cmd/boot/internal/consent"
	bootenv "go.astrophena.name/tools/cmd/boot/internal/env"
	bootfetch "go.astrophena.name/tools/cmd/boot/internal/fetch"
	bootflatpak "go.astrophena.name/tools/cmd/boot/internal/flatpak"
	bootfs "go.astrophena.name/tools/cmd/boot/internal/fs"
	bootgit "go.astrophena.name/tools/cmd/boot/internal/git"
	bootgo "go.astrophena.name/tools/cmd/boot/internal/golang"
	bootpkg "go.astrophena.name/tools/cmd/boot/internal/packages"
	bootprogram "go.astrophena.name/tools/cmd/boot/internal/program"
	bootrescue "go.astrophena.name/tools/cmd/boot/internal/rescue"
	bootssh "go.astrophena.name/tools/cmd/boot/internal/ssh"
	bootsystemd "go.astrophena.name/tools/cmd/boot/internal/systemd"

	"go.starlark.net/starlarkstruct"
	"golang.org/x/term"
)

const defaultEntry = "BOOT.star"

func main() { cli.Main(new(app)) }

type app struct {
	options
	flags *flag.FlagSet
	only  multiFlag
	skip  multiFlag
	tags  multiFlag
}

func (a *app) Flags(fs *flag.FlagSet) {
	a.flags = fs
	a.concurrency = 1
	fs.StringVar(&a.workspace, "C", ".", "Run as if boot was started in `dir`.")
	fs.StringVar(&a.entry, "f", defaultEntry, "Recipe entrypoint `file`.")
	fs.BoolVar(&a.failFast, "fail-fast", false, "Stop on the first failed task.")
	fs.BoolVar(&a.verbose, "verbose", false, "Print detailed action output when applying changes.")
	fs.BoolVar(&a.json, "json", false, "Print machine-readable JSON output.")
	fs.Var(&a.only, "only", "Run only the specified task ID. May be repeated.")
	fs.Var(&a.skip, "skip", "Skip the specified task ID. May be repeated.")
	fs.Var(&a.tags, "tag", "Run tasks with the specified tag. May be repeated.")
	fs.Func("j", "Number of tasks and eligible actions to run in parallel (default 1).", func(value string) error {
		n, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		if n < 1 {
			return errors.New("must be at least 1")
		}
		a.concurrency = n
		return nil
	})
}

func (a *app) Run(ctx context.Context) error {
	env := cli.GetEnv(ctx)
	if len(env.Args) != 1 {
		return fmt.Errorf("%w: command is required\nusage: boot [flags...] <list|plan|apply>\nrun 'boot -help' for details", cli.ErrInvalidArgs)
	}

	command := env.Args[0]
	if err := a.loadOptions(ctx, env); err != nil {
		return fmt.Errorf("%w: %v", cli.ErrInvalidArgs, err)
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
	opts := boot.RunOptions{
		FailFast:    a.failFast,
		Interactive: interactive(env),
		Concurrency: a.concurrency,
		Color:       env.Getenv("NO_COLOR") == "",
		Verbose:     a.verbose,
		JSON:        a.json,
	}
	switch command {
	case "list":
		return engine.List(env.Stdout, selection)
	case "plan", "check":
		opts.DryRun = true
	case "apply":
		lock, err := acquireLock(rootLockPath(engine.Runtime.Root))
		if err != nil {
			return err
		}
		defer lock.Close()
	default:
		return fmt.Errorf("%w: no such command %q", cli.ErrInvalidArgs, command)
	}
	return engine.Run(ctx, env.Stdout, selection, opts)
}

func rootLockPath(root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(os.TempDir(), "boot-"+hex.EncodeToString(sum[:8])+".lock")
}

func acquireLock(path string) (*os.File, error) {
	lock, err := filelock.Acquire(path)
	if err == nil {
		return lock, nil
	}
	if errors.Is(err, filelock.ErrLocked) {
		return nil, errors.New("another boot apply is already running for this recipe")
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

func (a *app) loadOptions(ctx context.Context, env *cli.Env) error {
	cfg := a.options
	if err := loadConfig(ctx, env, &cfg); err != nil {
		return err
	}

	cfg.workspace = expandHome(env, cfg.workspace)
	a.flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "C":
			cfg.workspace = a.workspace
		case "f":
			cfg.entry = a.entry
		case "j":
			cfg.concurrency = a.concurrency
		case "fail-fast":
			cfg.failFast = a.failFast
		case "verbose":
			cfg.verbose = a.verbose
		case "json":
			cfg.json = a.json
		}
	})
	a.options = cfg
	return nil
}

func (a *app) engine(env *cli.Env) (*boot.Engine, error) {
	root, err := filepath.Abs(a.workspace)
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
		Modules: []*starlarkstruct.Module{
			bootconsent.Module(rt),
			bootenv.Module(rt),
			bootfetch.Module(rt),
			bootflatpak.Module(rt),
			bootfs.Module(rt),
			bootpkg.Module(rt),
			bootpkg.PacmanModule(rt),
			bootprogram.Module(rt),
			bootgit.Module(rt),
			bootgo.Module(rt),
			bootrescue.Module(rt),
			bootssh.Module(rt),
			bootsystemd.Module(rt),
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

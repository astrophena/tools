// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Dupfind finds duplicate files in a directory.
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"go.astrophena.name/tools/internal/cli"
)

func main() {
	cli.Run(func(_ context.Context) error {
		return run(os.Args[1:], os.Stdout, os.Stderr)
	})
}

func run(args []string, stdout, stderr io.Writer) error {
	a := &cli.App{
		Name:        "dupfind",
		Description: helpDoc,
		ArgsUsage:   "[flags...] <dir>",
	}
	if err := a.HandleStartup(args, stdout, stderr); err != nil {
		if errors.Is(err, cli.ErrExitVersion) {
			return nil
		}
		return err
	}

	if len(a.Flags.Args()) != 1 {
		a.Flags.Usage()
		return fmt.Errorf("%w: missing required argument 'dir'", cli.ErrInvalidArgs)
	}

	dir := a.Flags.Args()[0]

	if realdir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realdir
	}

	dups, err := lookup(dir)
	if err != nil {
		return err
	}

	for _, d := range dups {
		fmt.Fprintf(stderr, "Duplicate file %s of %s.\n", d.cur, d.prev)
	}

	return nil
}

type dup struct {
	cur, prev string
}

func lookup(dir string) ([]dup, error) {
	var (
		dups   []dup
		hashes = make(map[string]string)
	)

	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}

		hh := fmt.Sprintf("%x", h.Sum(nil))

		prev, hasDup := hashes[hh]
		if hasDup {
			bpath, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			bprev, err := filepath.Rel(dir, prev)
			if err != nil {
				return err
			}

			dups = append(dups, dup{
				cur:  bpath,
				prev: bprev,
			})
			return nil
		}

		hashes[hh] = path
		return nil
	}); err != nil {
		return nil, err
	}

	return dups, nil
}

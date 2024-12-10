// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/cli/restrict"

	"github.com/landlock-lsm/go-landlock/landlock"
)

func main() { cli.Main(cli.AppFunc(run)) }

func run(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	if len(env.Args) != 1 {
		return fmt.Errorf("%w: missing required argument 'dir'", cli.ErrInvalidArgs)
	}

	dir := env.Args[0]

	if realdir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realdir
	}

	// Drop privileges if not inside tests.
	if !testing.Testing() {
		restrict.Do(ctx, landlock.RODirs(dir))
	}

	dups, err := lookup(dir)
	if err != nil {
		return err
	}

	for _, d := range dups {
		fmt.Fprintf(env.Stderr, "Duplicate file %s of %s.\n", d.cur, d.prev)
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

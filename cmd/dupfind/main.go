// Dupfind finds duplicate files in a directory.
package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"go.astrophena.name/tools/internal/cli"
)

func main() {
	cli.SetArgsUsage("[flags...] <dir>")
	cli.HandleStartup()

	if len(cli.Args()) != 1 {
		cli.Flags.Usage()
		os.Exit(1)
	}
	dir := cli.Args()[0]

	if realdir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realdir
	}

	dups, err := lookup(dir)
	if err != nil {
		log.Fatal(err)
	}

	for _, d := range dups {
		log.Printf("duplicate file %s of %s", d.cur, d.prev)
	}
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

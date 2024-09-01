// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Renamer renames files sequentially.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"go.astrophena.name/base/logger"
	"go.astrophena.name/tools/internal/cli"
)

func main() { cli.Run(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) error {
	a := &cli.App{
		Name:        "renamer",
		Description: helpDoc,
		ArgsUsage:   "[flags...] <dir>",
		Flags:       flag.NewFlagSet("renamer", flag.ContinueOnError),
	}
	var (
		start = a.Flags.Int("start", 1, "Start from `number`.")
	)
	if err := a.HandleStartup(args, stdout, stderr); err != nil {
		if errors.Is(err, cli.ErrExitVersion) {
			return nil
		}
		return err
	}

	if len(a.Flags.Args()) != 1 {
		a.Flags.Usage()
		return cli.ErrArgsNeeded
	}
	dir := a.Flags.Args()[0]
	if realdir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realdir
	}

	return rename(dir, *start, log.New(stderr, "", 0).Printf)
}

func rename(dir string, start int, logf logger.Logf) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			return nil
		}

		var (
			ext = filepath.Ext(path)
			dir = filepath.Dir(path)

			oldname = filepath.Join(dir, filepath.Base(path))
			newname = filepath.Join(dir, fmt.Sprintf("%d%s", start, ext))
		)

		if _, err := os.Stat(newname); !errors.Is(err, fs.ErrNotExist) {
			logf("File %s already exists, skipping.", newname)
			start++
			return nil
		}

		logf("Renaming %s to %s.", oldname, newname)
		if err := os.Rename(oldname, newname); err != nil {
			return err
		}

		start++
		return nil
	})
}

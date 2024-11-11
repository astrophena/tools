// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Renamer renames files in a specified directory sequentially, starting from a
// given number.
//
// It sorts the files based on name, time, size, or type before renaming. If a
// file with the new name already exists, it skips the file and continues to the
// next.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"

	"go.astrophena.name/base/logger"
	"go.astrophena.name/tools/internal/cli"
)

func main() {
	cli.Run(func(_ context.Context) error {
		return run(os.Args[1:], os.Stdout, os.Stderr)
	})
}

var errUnknownSortMode = errors.New("unknown sort mode")

func run(args []string, stdout, stderr io.Writer) error {
	a := &cli.App{
		Name:        "renamer",
		Description: helpDoc,
		ArgsUsage:   "[flags...] <dir>",
		Flags:       flag.NewFlagSet("renamer", flag.ContinueOnError),
	}
	var (
		dry   = a.Flags.Bool("dry", false, "Print what would be done, but don't rename files.")
		sort  = a.Flags.String("sort", "name", "Sort files by `name, time, size or type`.")
		start = a.Flags.Int("start", 1, "Start numbering files from this `number`.")
	)
	if err := a.HandleStartup(args, stdout, stderr); err != nil {
		if errors.Is(err, cli.ErrExitVersion) {
			return nil
		}
		return err
	}

	if len(a.Flags.Args()) != 1 {
		a.Flags.Usage()
		return fmt.Errorf("%w: exactly one directory argument is required", cli.ErrInvalidArgs)
	}
	dir := a.Flags.Args()[0]
	if realdir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realdir
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var fis = map[string]fs.FileInfo{}
	for _, f := range files {
		fi, err := f.Info()
		if err != nil {
			return err
		}
		fis[f.Name()] = fi
	}
	getFileInfo := func(de fs.DirEntry) fs.FileInfo {
		return fis[de.Name()]
	}

	switch *sort {
	case "name":
		slices.SortStableFunc(files, func(a, b fs.DirEntry) int {
			return cmp.Compare(a.Name(), b.Name())
		})
	case "time":
		slices.SortStableFunc(files, func(a, b fs.DirEntry) int {
			return cmp.Compare(getFileInfo(a).ModTime().Unix(), getFileInfo(b).ModTime().Unix())
		})
	case "size":
		slices.SortStableFunc(files, func(a, b fs.DirEntry) int {
			return cmp.Compare(getFileInfo(a).Size(), getFileInfo(b).Size())
		})
	case "type":
		slices.SortStableFunc(files, func(a, b fs.DirEntry) int {
			return cmp.Compare(filepath.Ext(a.Name()), filepath.Ext(b.Name()))
		})
	default:
		return errUnknownSortMode
	}

	return rename(*dry, dir, files, *start, log.New(stderr, "", 0).Printf)
}

func rename(dry bool, dir string, files []fs.DirEntry, start int, logf logger.Logf) error {
	for _, d := range files {
		if d.IsDir() {
			return nil
		}

		var (
			ext     = filepath.Ext(d.Name())
			oldname = filepath.Join(dir, d.Name())
			newname = filepath.Join(dir, fmt.Sprintf("%d%s", start, ext))
		)

		if _, err := os.Stat(newname); !errors.Is(err, fs.ErrNotExist) {
			logf("File %s already exists, skipping.", newname)
			start++
			continue
		}

		if dry {
			logf("Would rename %s to %s.", oldname, newname)
		} else {
			logf("Renaming %s to %s.", oldname, newname)
			if err := os.Rename(oldname, newname); err != nil {
				return err
			}
		}

		start++
	}
	return nil
}

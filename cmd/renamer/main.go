// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Renamer renames files in a specified directory sequentially, starting from a
// given number.
//
// # Usage
//
//	$ renamer [flags...] <dir>
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
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"

	"go.astrophena.name/base/logger"
	"go.astrophena.name/tools/internal/cli"
)

func main() { cli.Main(new(app)) }

type app struct {
	dry   bool
	sort  string
	start int
	logf  logger.Logf
}

func (a *app) Flags(fs *flag.FlagSet) {
	fs.BoolVar(&a.dry, "dry", false, "Print what would be done, but don't rename files.")
	fs.StringVar(&a.sort, "sort", "name", "Sort files by `name, time, size or type`.")
	fs.IntVar(&a.start, "start", 1, "Start numbering files from this `number`.")
}

var errUnknownSortMode = errors.New("unknown sort mode")

func (a *app) Run(_ context.Context, env cli.Env) error {
	if a.logf == nil {
		a.logf = log.New(env.Stderr, "", 0).Printf
	}

	if len(env.Args) != 1 {
		return fmt.Errorf("%w: exactly one directory argument is required", cli.ErrInvalidArgs)
	}
	dir := env.Args[0]
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

	switch a.sort {
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

	return a.rename(dir, files)
}

func (a *app) rename(dir string, files []fs.DirEntry) error {
	for _, d := range files {
		if d.IsDir() {
			return nil
		}

		var (
			ext     = filepath.Ext(d.Name())
			oldname = filepath.Join(dir, d.Name())
			newname = filepath.Join(dir, fmt.Sprintf("%d%s", a.start, ext))
		)

		if _, err := os.Stat(newname); !errors.Is(err, fs.ErrNotExist) {
			a.logf("File %s already exists, skipping.", newname)
			a.start++
			continue
		}

		if a.dry {
			a.logf("Would rename %s to %s.", oldname, newname)
		} else {
			a.logf("Renaming %s to %s.", oldname, newname)
			if err := os.Rename(oldname, newname); err != nil {
				return err
			}
		}

		a.start++
	}
	return nil
}

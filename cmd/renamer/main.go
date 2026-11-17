// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/logger"
	"go.astrophena.name/tools/internal/restrict"

	"github.com/landlock-lsm/go-landlock/landlock"
)

func main() { cli.Main(new(app)) }

type app struct {
	dry   bool
	sort  string
	start int
}

func (a *app) Flags(fs *flag.FlagSet) {
	fs.BoolVar(&a.dry, "dry", false, "Print what would be done, but don't rename files.")
	fs.StringVar(&a.sort, "sort", "name", "Sort files by `name, time, size or type`.")
	fs.IntVar(&a.start, "start", 1, "Start numbering files from this `number`.")
}

var errUnknownSortMode = errors.New("unknown sort mode")

func (a *app) Run(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	if len(env.Args) != 1 {
		return fmt.Errorf("%w: exactly one directory argument is required", cli.ErrInvalidArgs)
	}
	dir := env.Args[0]
	if realdir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realdir
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	restrict.DoUnlessTesting(ctx, landlock.RWDirs(dir))

	files, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	switch a.sort {
	case "name":
		// ReadDir already sorts by name.
	case "time", "size":
		keys := make(map[string]int64, len(files))
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			info, err := f.Info()
			if err != nil {
				return err
			}
			if a.sort == "time" {
				keys[f.Name()] = info.ModTime().Unix()
			} else {
				keys[f.Name()] = info.Size()
			}
		}
		slices.SortStableFunc(files, func(a, b fs.DirEntry) int {
			return cmp.Compare(keys[a.Name()], keys[b.Name()])
		})
	case "type":
		slices.SortStableFunc(files, func(a, b fs.DirEntry) int {
			return cmp.Compare(filepath.Ext(a.Name()), filepath.Ext(b.Name()))
		})
	default:
		return errUnknownSortMode
	}

	return a.rename(ctx, root, dir, files)
}

func (a *app) rename(ctx context.Context, root *os.Root, dir string, files []fs.DirEntry) error {
	for _, d := range files {
		if d.IsDir() {
			continue
		}

		var (
			ext        = filepath.Ext(d.Name())
			oldName    = d.Name()
			newName    = fmt.Sprintf("%d%s", a.start, ext)
			oldLogPath = filepath.Join(dir, oldName)
			newLogPath = filepath.Join(dir, newName)
		)

		switch _, err := root.Stat(newName); {
		case err == nil:
			logger.Info(ctx, "file already exists, skipping", slog.String("path", newLogPath))
			a.start++
			continue
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}

		if a.dry {
			logger.Info(ctx, "would rename", slog.String("from", oldLogPath), slog.String("to", newLogPath))
		} else {
			logger.Info(ctx, "renaming", slog.String("from", oldLogPath), slog.String("to", newLogPath))
			if err := root.Rename(oldName, newName); err != nil {
				return err
			}
		}

		a.start++
	}
	return nil
}

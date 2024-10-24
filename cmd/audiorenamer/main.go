// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Audiorenamer traverses a directory and renames music tracks based on their
// metadata. It extracts the track number and title from the files' metadata.
// If the title contains slashes, it strips them out to create a valid filename.
// The new filename format is "<track number>. <title>.<extension>".
//
// The program takes a directory path as an required argument.
//
// Running it on my music collection:
//
//	$ audiorenamer ~/media/music
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"

	"go.astrophena.name/tools/internal/cli"
)

func main() {
	cli.Run(func(_ context.Context) error {
		return run(os.Args[1:], os.Stdout, os.Stderr)
	})
}

func run(args []string, stdout, stderr io.Writer) error {
	// Define and parse flags.
	a := &cli.App{
		Name:        "audiorenamer",
		Description: helpDoc,
		Credits:     credits,
		ArgsUsage:   "[flags...] <dir>",
		Flags:       flag.NewFlagSet("audiorenamer", flag.ContinueOnError),
	}
	var (
		dryRun  = a.Flags.Bool("dry", false, "Print what will be done, but don't do anything.")
		verbose = a.Flags.Bool("verbose", false, "Print all log messages.")
	)
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

	logf := log.New(stderr, "", 0).Printf
	vlog := func(format string, args ...any) {
		if !*dryRun {
			if !*verbose {
				return
			}
			return
		}
		logf(format, args...)
	}

	var processed, existing, renamed int

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

		if format, typ, err := tag.Identify(f); typ == "" || format == "" {
			// Unknown file or tag format, skip.
			return nil
		} else if err != nil {
			return fmt.Errorf("identifying %q: %v", path, err)
		}

		m, err := tag.ReadFrom(f)
		if err != nil {
			return fmt.Errorf("reading tags from %q: %v", path, err)
		}

		processed++

		dir := filepath.Dir(path)

		num, _ := m.Track()
		title := m.Title()

		// Strip slashes from the title to make it a valid filename.
		title = strings.ReplaceAll(title, "/", "")

		newname := filepath.Join(dir, fmt.Sprintf("%d. %s%s", num, title, filepath.Ext(path)))

		if path == newname {
			vlog("Already exists: %q, no need to rename.", path)
			existing++
			return nil
		}

		logMsg := "Renaming"
		if *dryRun {
			logMsg = "Will rename"
		}
		vlog("%s: %q -> %q.", logMsg, path, newname)
		if !*dryRun {
			if err := os.Rename(path, newname); err != nil {
				return err
			}
		}
		renamed++

		return nil
	}); err != nil {
		return err
	}

	var msg string
	if *dryRun {
		msg += "Dry run: "
	}
	msg += "%d processed: %d renamed, %d existing."
	logf(msg, processed, renamed, existing)

	return nil
}

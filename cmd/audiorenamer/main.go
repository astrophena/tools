// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/dhowden/tag"
	"github.com/landlock-lsm/go-landlock/landlock"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/tools/internal/restrict"
)

func main() { cli.Main(new(app)) }

var errInvalidFormat = errors.New("invalid format")

type app struct {
	dry     bool
	format  string
	verbose bool
}

func (a *app) Flags(fs *flag.FlagSet) {
	fs.BoolVar(&a.dry, "dry", false, "Print the renaming operations without actually renaming files.")
	fs.StringVar(&a.format, "format", "{{ track . }}. {{ .Title }}", "Go `template` to format the filename.\n"+
		"See https://pkg.go.dev/github.com/dhowden/tag#Metadata for available fields.")
	fs.BoolVar(&a.verbose, "verbose", false, "Print all log messages, including those for files that are not renamed.")
}

func (a *app) Run(ctx context.Context) error {
	tmpl, err := template.New("main").Funcs(template.FuncMap{
		"track": func(m tag.Metadata) int {
			num, _ := m.Track()
			return num
		},
	}).Parse(a.format)
	if err != nil {
		return fmt.Errorf("%w: %v", errInvalidFormat, err)
	}

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
		restrict.Do(ctx, landlock.RWDirs(dir))
	}

	vlog := func(format string, args ...any) {
		if !a.dry {
			if !a.verbose {
				return
			}
			return
		}
		env.Logf(format, args...)
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

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, m); err != nil {
			return err
		}
		// Strip slashes from the new name to make it a valid filename.
		newname := strings.ReplaceAll(buf.String(), "/", "")
		newname = filepath.Join(dir, newname+filepath.Ext(path))

		if path == newname {
			vlog("Already exists: %q, no need to rename.", path)
			existing++
			return nil
		}

		logMsg := "Renaming"
		if a.dry {
			logMsg = "Would rename"
		}
		vlog("%s: %q -> %q.", logMsg, path, newname)
		if !a.dry {
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
	if a.dry {
		msg += "Dry run: "
	}
	msg += "%d processed: %d renamed, %d existing."
	env.Logf(msg, processed, renamed, existing)

	return nil
}

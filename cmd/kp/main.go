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
	"io"
	"os"
	"syscall"
	"text/template"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/tools/internal/restrict"

	"github.com/landlock-lsm/go-landlock/landlock"
	"github.com/tobischo/gokeepasslib/v3"
	"golang.org/x/term"
)

func main() { cli.Main(new(app)) }

var (
	errInvalidFormat = errors.New("invalid format")
	errNotFound      = errors.New("not found")
	errFailOpen      = errors.New("failed to open")
)

type app struct {
	format string
	list   bool
}

func (a *app) Flags(fs *flag.FlagSet) {
	fs.StringVar(&a.format, "f", "{{ .GetPassword }}", "Format Go `template`.")
	fs.BoolVar(&a.list, "l", false, "List all entries.")
}

func (a *app) Run(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	tmpl, err := template.New("main").Parse(a.format)
	if err != nil {
		return fmt.Errorf("%w: %v", errInvalidFormat, err)
	}

	if len(env.Args) == 0 {
		return fmt.Errorf("%w: missing required argument 'file'", cli.ErrInvalidArgs)
	} else if !a.list && len(env.Args) < 2 {
		return fmt.Errorf("%w: missing required arguments 'file' and/or 'entry'", cli.ErrInvalidArgs)
	}

	file := env.Args[0]

	// Drop privileges if not inside tests.
	restrict.DoUnlessTesting(ctx, landlock.ROFiles(file))

	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	password := env.Getenv("KP_PASSWORD")
	if password == "" {
		password, err = ask(file, env.Stderr)
		if err != nil {
			return err
		}
	}

	if a.list {
		db, err := open(f, password)
		if err != nil {
			return fmt.Errorf("%w: %v", errFailOpen, err)
		}
		for _, g := range db.Content.Root.Groups {
			for _, e := range g.Entries {
				if err := printEntry(tmpl, &e, env.Stdout); err != nil {
					return err
				}
			}
		}
		return nil
	}

	e, err := lookup(f, password, env.Args[1])
	if err != nil {
		return err
	}
	if err := printEntry(tmpl, e, env.Stdout); err != nil {
		return err
	}

	return nil
}

func printEntry(tmpl *template.Template, e *gokeepasslib.Entry, stdout io.Writer) error {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, e); err != nil {
		return err
	}
	buf.WriteString("\n")
	buf.WriteTo(stdout)
	return nil
}

func open(r io.Reader, password string) (*gokeepasslib.Database, error) {
	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials(password)
	if err := gokeepasslib.NewDecoder(r).Decode(db); err != nil {
		return nil, err
	}
	if err := db.UnlockProtectedEntries(); err != nil {
		return nil, err
	}
	return db, nil
}

func lookup(r io.Reader, password string, entry string) (*gokeepasslib.Entry, error) {
	db, err := open(r, password)
	if err != nil {
		return nil, err
	}
	for _, g := range db.Content.Root.Groups {
		if e := findEntry(g, entry); e != nil {
			return e, nil
		}
	}
	return nil, fmt.Errorf("entry %q %w", entry, errNotFound)
}

func findEntry(g gokeepasslib.Group, title string) *gokeepasslib.Entry {
	for _, e := range g.Entries {
		if e.GetTitle() == title {
			return &e
		}
	}
	for _, sg := range g.Groups {
		if e := findEntry(sg, title); e != nil {
			return e
		}
	}
	return nil
}

func ask(file string, stderr io.Writer) (password string, err error) {
	fmt.Fprintf(stderr, "Password for %s (will not be visible on the screen): ", file)
	b, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", err
	}
	fmt.Fprintf(stderr, "\n")
	return string(b), nil
}

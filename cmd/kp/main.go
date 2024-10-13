// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Kp reads passwords from KeePass databases.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"os"
	"syscall"

	"go.astrophena.name/tools/internal/cli"

	"github.com/tobischo/gokeepasslib/v3"
	"golang.org/x/term"
)

func main() {
	cli.Run(func(_ context.Context) error {
		return run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	})
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	a := &cli.App{
		Name:        "kp",
		Description: helpDoc,
		ArgsUsage:   "[flags...] <file> [entry]",
		Flags:       flag.NewFlagSet("kp", flag.ContinueOnError),
	}
	var (
		format = a.Flags.String("f", "{{ .GetPassword }}", "format `template`")
		list   = a.Flags.Bool("l", false, "list all entries")
	)
	if err := a.HandleStartup(args, stdout, stderr); err != nil {
		if errors.Is(err, cli.ErrExitVersion) {
			return nil
		}
		return err
	}

	tmpl, err := template.New("main").Parse(*format)
	if err != nil {
		return fmt.Errorf("invalid format: %v", err)
	}

	fargs := a.Flags.Args()
	if len(fargs) == 0 {
		a.Flags.Usage()
		return fmt.Errorf("%w: missing required argument 'file'", cli.ErrInvalidArgs)
	} else if !*list && len(fargs) < 2 {
		a.Flags.Usage()
		return fmt.Errorf("%w: missing required arguments 'file' and/or 'entry'", cli.ErrInvalidArgs)
	}

	file := fargs[0]

	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	password, err := ask(file, stderr)
	if err != nil {
		return err
	}

	if *list {
		db, err := open(f, password)
		if err != nil {
			return err
		}
		for _, g := range db.Content.Root.Groups {
			for _, e := range g.Entries {
				if err := printEntry(tmpl, &e, stdout); err != nil {
					return err
				}
			}
		}
		return nil
	}

	e, err := lookup(f, password, fargs[1])
	if err != nil {
		return err
	}
	if err := printEntry(tmpl, e, stdout); err != nil {
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

var errNotFound = errors.New("not found")

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
	fmt.Fprintf(stderr, "Password for %s (will not shown): ", file)
	b, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", err
	}
	fmt.Fprintf(stderr, "\n")
	return string(b), nil
}

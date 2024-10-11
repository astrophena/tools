// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Kp reads passwords from KeePass databases.
package main

import (
	"context"
	"errors"
	"fmt"
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
		ArgsUsage:   "[flags...] <file> <entry>",
	}
	if err := a.HandleStartup(args, stdout, stderr); err != nil {
		if errors.Is(err, cli.ErrExitVersion) {
			return nil
		}
		return err
	}

	fargs := a.Flags.Args()
	if len(fargs) != 2 {
		a.Flags.Usage()
		return fmt.Errorf("%w: missing required arguments 'file' and/or 'entry'", cli.ErrInvalidArgs)
	}

	file, entry := fargs[0], fargs[1]

	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	password, err := ask(file, stderr)
	if err != nil {
		return err
	}

	e, err := lookup(f, password, entry)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, e.GetPassword())

	return nil
}

var errNotFound = errors.New("not found")

func lookup(r io.Reader, password string, entry string) (*gokeepasslib.Entry, error) {
	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials(password)
	if err := gokeepasslib.NewDecoder(r).Decode(db); err != nil {
		return nil, err
	}
	if err := db.UnlockProtectedEntries(); err != nil {
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

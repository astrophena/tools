// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"go.astrophena.name/base/txtar"
	"go.astrophena.name/tools/internal/cli"
)

func main() { cli.Main(new(app)) }

type app struct {
	create bool
}

func (a *app) Flags(fs *flag.FlagSet) {
	fs.BoolVar(&a.create, "c", false, "Create txtar archive instead of extracting.")
}

func (a *app) Run(ctx context.Context, env *cli.Env) error {
	if len(env.Args) != 2 {
		return fmt.Errorf("%w: missing required arguments 'file' and 'directory'", cli.ErrInvalidArgs)
	}
	file, dir := env.Args[0], env.Args[1]

	if a.create {
		ar, err := txtar.FromDir(dir)
		if err != nil {
			return err
		}
		return os.WriteFile(file, txtar.Format(ar), 0o644)
	}

	ar, err := txtar.ParseFile(file)
	if err != nil {
		return err
	}
	return txtar.Extract(ar, dir)
}

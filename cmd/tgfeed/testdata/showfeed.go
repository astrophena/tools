// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:build ignore

package main

import (
	"context"
	"fmt"

	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/util/starlarkconv"

	"github.com/mmcdole/gofeed"
)

func main() { cli.Main(cli.AppFunc(run)) }

func run(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	if len(env.Args) == 0 {
		return fmt.Errorf("%w: url is required", cli.ErrInvalidArgs)
	}
	url := env.Args[0]

	p := gofeed.NewParser()

	f, err := p.ParseURLWithContext(url, ctx)
	if err != nil {
		return err
	}

	for _, item := range f.Items {
		val, err := starlarkconv.ToValue(*item)
		if err != nil {
			return err
		}
		fmt.Fprintf(env.Stdout, val.String())
	}

	return nil
}

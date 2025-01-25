// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"

	"go.astrophena.name/base/cli"

	"github.com/muesli/reflow/wordwrap"
	"rsc.io/markdown"
)

func main() { cli.Main(new(app)) }

type app struct {
	// flags
	lineLength int
	rewrite    bool
}

var parser = sync.OnceValue(func() *markdown.Parser {
	return &markdown.Parser{
		HeadingID:          true,
		Strikethrough:      true,
		TaskList:           true,
		AutoLinkText:       true,
		AutoLinkAssumeHTTP: true,
		Table:              true,
		Emoji:              true,
		SmartDot:           true,
		SmartDash:          true,
		SmartQuote:         true,
		Footnote:           true,
	}
})

func (a *app) Flags(fs *flag.FlagSet) {
	fs.IntVar(&a.lineLength, "line-length", 120, "Line length `limit`.")
	fs.BoolVar(&a.rewrite, "w", false, "Write result to (source) file instead of stdout.")
}

func (a *app) Run(ctx context.Context) error {
	env := cli.GetEnv(ctx)

	if len(env.Args) == 0 {
		return fmt.Errorf("%w: at least one file required", cli.ErrInvalidArgs)
	}

	for _, file := range env.Args {
		b, err := a.format(file)
		if err != nil {
			return fmt.Errorf("formatting %q: %w", file, err)
		}
		if a.rewrite {
			perm := fs.FileMode(0o644)
			if fi, err := os.Stat(file); err == nil {
				perm = fi.Mode().Perm()
			}
			if err := os.WriteFile(file, b, perm); err != nil {
				return err
			}
		} else {
			env.Stdout.Write(b)
		}
	}

	return nil
}

func (a *app) format(file string) ([]byte, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	doc := parser().Parse(string(b))

	var sb strings.Builder

	for _, b := range doc.Blocks {
		sb.WriteString("\n")
		sb.WriteString(wordwrap.String(markdown.Format(b), a.lineLength))
		sb.WriteString("\n")
	}

	return []byte(strings.TrimSpace(sb.String()) + "\n"), nil
}

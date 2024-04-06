// nora is a Nora programming language interpreter and REPL.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/nora/lex"
	"go.astrophena.name/tools/internal/nora/parse"
	"go.astrophena.name/tools/internal/nora/repl"
	"go.astrophena.name/tools/internal/nora/token"
	"go.astrophena.name/tools/internal/version"

	"github.com/davecgh/go-spew/spew"
)

func main() {
	var (
		dumpAST = flag.String("dump-ast", "", "Dump AST of a Nora program from `file`.")
	)
	cli.SetDescription("nora is a Nora programming language interpreter and REPL.")
	cli.SetArgsUsage("[*.nora]")
	cli.HandleStartup()

	if *dumpAST != "" {
		b, err := os.ReadFile(*dumpAST)
		if err != nil {
			log.Fatal(err)
		}

		l := lex.New(string(b))
		p := parse.New(l)

		prog, err := p.ParseProgram()
		if err != nil {
			log.Fatal(err)
		}

		spew.Fdump(os.Stdout, prog)

		return
	}

	if len(flag.Args()) > 0 {
		file := flag.Args()[0]

		b, err := os.ReadFile(file)
		if err != nil {
			log.Fatal(err)
		}

		l := lex.New(string(b))
		for tok := l.NextToken(); tok.Type != token.EOF; tok = l.NextToken() {
			fmt.Fprintf(os.Stdout, "%+v\n", tok)
		}

		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	log.Println(version.Version().Short())
	log.Println("Use Ctrl+D to exit.")
	if err := repl.Start(ctx, os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

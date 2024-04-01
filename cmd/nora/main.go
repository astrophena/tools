// nora is a Nora programming language interpreter and REPL.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/nora/repl"
)

func main() {
	cli.SetDescription("nora is a Nora programming language interpreter and REPL.")
	cli.SetArgsUsage("[*.nora]")
	cli.HandleStartup()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	log.Println("Use Ctrl+D to exit.")
	if err := repl.Start(ctx, os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

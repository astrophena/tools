// Txtar is a command-line tool for creating and extracting txtar archives.
//
// It can be used in two ways:
//
// To create a txtar archive:
//
//	$ txtar -c <archive> <directory>
//
// This will create a txtar archive named <archive> containing all the files and
// directories from the specified <directory>.
//
// To extract a txtar archive:
//
//	$ txtar <archive> <directory>
//
// This will extract the contents of the txtar archive named <archive> into the
// specified directory.
package main

import (
	"flag"
	"log"
	"os"

	"go.astrophena.name/tools/internal/cli"
	"go.astrophena.name/tools/internal/testutil/txtar"
)

func main() {
	var (
		createFlag = flag.Bool("c", false, "Create txtar archive instead of extracting.")
	)
	cli.SetArgsUsage("[flags] <archive> <directory>")
	cli.HandleStartup()

	args := flag.Args()
	if len(args) != 2 {
		flag.Usage()
		os.Exit(1)
	}
	file, dir := args[0], args[1]

	if *createFlag {
		ar, err := txtar.FromDir(dir)
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(file, txtar.Format(ar), 0o644); err != nil {
			log.Fatal(err)
		}
		return
	}

	ar, err := txtar.ParseFile(file)
	if err != nil {
		log.Fatal(err)
	}
	if err := txtar.Extract(ar, dir); err != nil {
		log.Fatal(err)
	}
}

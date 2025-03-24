// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE file.

// New generates a scaffold of the new application.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"go.astrophena.name/base/txtar"
	"go.astrophena.name/tools/internal/devtools"
)

//go:embed template.txtar
var templateTxtar []byte

func main() {
	devtools.EnsureRoot()

	log.SetFlags(0)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: go tool new [flags] [name]\n")
	}
	flag.Parse()

	if len(flag.Args()) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	name := flag.Args()[0]
	path := filepath.Join("cmd", name)
	if _, err := os.Stat(path); err == nil {
		log.Fatalf("%s already does exist", name)
	}

	if err := os.MkdirAll(path, 0o755); err != nil {
		log.Fatal(err)
	}
	ar := txtar.Parse(templateTxtar)
	if err := txtar.Extract(ar, path); err != nil {
		log.Fatal(err)
	}
	log.Printf("%s successfully created.", name)
}

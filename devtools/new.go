//usr/bin/env go run $0 $@; exit $?

// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE file.

//go:build ignore

// new.go generates a scaffold of the new application.

package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"go.astrophena.name/base/txtar"
)

//go:embed app.txtar
var templateTxtar []byte

func main() {
	log.SetFlags(0)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: ./devtools/new.go [flags] [name]\n")
	}
	flag.Parse()

	wd := try(os.Getwd())
	if _, err := os.Stat(filepath.Join(wd, "go.mod")); errors.Is(err, fs.ErrNotExist) {
		log.Fatal("Are you at repo root?")
	} else if err != nil {
		log.Fatal(err)
	}

	if len(flag.Args()) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	name := flag.Args()[0]
	path := filepath.Join("cmd", name)
	if _, err := os.Stat(path); err == nil {
		log.Fatalf("%s already does exist", name)
	}

	must(os.MkdirAll(path, 0o755))
	ar := txtar.Parse(templateTxtar)
	must(txtar.Extract(ar, path))
	log.Printf("%s successfully created.", name)
}

func try[T any](val T, err error) T {
	must(err)
	return val
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

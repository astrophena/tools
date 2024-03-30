// dupfind finds duplicate files in a directory.
package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"go.astrophena.name/tools/internal/cli"
)

func main() {
	cli.SetDescription("dupfind finds duplicate files in a directory.")
	cli.SetArgsUsage("[dir]")
	cli.HandleStartup()

	if len(flag.Args()) != 1 {
		flag.Usage()
		os.Exit(1)
	}
	dir := flag.Args()[0]

	hashes := make(map[string]string)

	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}

		hh := fmt.Sprintf("%x", h.Sum(nil))

		prev, dup := hashes[hh]
		if dup {
			log.Printf("duplicate file %s, previously was at %s", path, prev)
			return nil
		}

		hashes[hh] = path
		return nil
	}); err != nil {
		log.Fatal(err)
	}
}

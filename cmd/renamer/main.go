// Renamer renames files sequentially.
package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"go.astrophena.name/tools/internal/cli"
)

func main() {
	var (
		dir   = cli.Flags.String("dir", ".", "Modify files in `path`.")
		start = cli.Flags.Int("start", 1, "Start from `number`.")
	)
	cli.HandleStartup()

	fullDir, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Are you sure? This will sequentially rename all files in %s. ", fullDir)
	if !askForConfirmation(os.Stdin) {
		log.Printf("Canceled.")
		return
	}

	if err := rename(fullDir, *start); err != nil {
		log.Fatal(err)
	}
}

func askForConfirmation(r io.Reader) bool {
	var response string

	_, err := fmt.Fscanln(r, &response)
	if err != nil {
		log.Fatal(err)
	}

	switch strings.ToLower(response) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		fmt.Println("I'm sorry but I didn't get what you meant, please type (y)es or (n)o and then press Enter:")
		return askForConfirmation(r)
	}
}

var logf = log.Printf // changed in tests

func rename(dir string, start int) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			return nil
		}

		var (
			ext = filepath.Ext(path)
			dir = filepath.Dir(path)

			oldname = filepath.Join(dir, filepath.Base(path))
			newname = filepath.Join(dir, fmt.Sprintf("%d%s", start, ext))
		)

		if _, err := os.Stat(newname); !errors.Is(err, fs.ErrNotExist) {
			logf("File %s already exists, skipping.", newname)
			start++
			return nil
		}

		logf("Renaming %s to %s.", oldname, newname)
		if err := os.Rename(oldname, newname); err != nil {
			return err
		}

		start++
		return nil
	})
}

// Audiorenamer traverses a directory and renames music tracks based on their
// metadata. It extracts the track number and title from the files' metadata.
// If the title contains slashes, it strips them out to create a valid filename.
// The new filename format is "<track number>. <title>.<extension>".
//
// The program takes a directory path as an required argument.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"

	"go.astrophena.name/tools/internal/cli"
)

func main() {
	cli.SetArgsUsage("[flags...] <dir>")
	cli.HandleStartup()

	if len(flag.Args()) != 1 {
		flag.Usage()
		os.Exit(1)
	}
	dir := flag.Args()[0]

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

		m, err := tag.ReadFrom(f)
		if err != nil {
			return err
		}

		dir := filepath.Dir(path)

		n, _ := m.Track()
		title := m.Title()

		// Strip slashes from the title to make it a valid filename
		title = strings.ReplaceAll(title, "/", "")

		newname := filepath.Join(dir, fmt.Sprintf("%d. %s.mp3", n, title))
		log.Printf("%q -> %q", path, newname)

		if err := os.Rename(path, newname); err != nil {
			return err
		}

		return nil
	}); err != nil {
		log.Fatal(err)
	}
}

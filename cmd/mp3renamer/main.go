// mp3renamer renames MP3 files by their track number and title.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"go.astrophena.name/tools/internal/cli"

	"github.com/dhowden/tag"
)

func main() {
	cli.SetDescription("mp3renamer renames MP3 files by their track number and title.")
	cli.SetArgsUsage("[dir]")
	cli.HandleStartup()

	dir := "."
	if len(flag.Args()) > 0 {
		dir = flag.Args()[0]
	}

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
		log.Printf(title)
		if title == "Alone / With You" {
			title = "Alone With You"
		}
		if title == "Still / Sound" {
			title = "Still Sound"
		}

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

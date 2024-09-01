// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:build ignore

// gencopyright.go adds copyright header to each Go file.

package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

const tmpl = `// © %d Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

`

func main() {
	if err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if bytes.HasPrefix(content, []byte("// ©")) {
			return nil // Already has a copyright header
		}

		year := info.ModTime().Year()
		header := fmt.Sprintf(tmpl, year)

		var buf bytes.Buffer
		buf.WriteString(header)
		buf.Write(content)

		return os.WriteFile(path, buf.Bytes(), 0o644)
	}); err != nil {
		log.Fatal(err)
	}
}

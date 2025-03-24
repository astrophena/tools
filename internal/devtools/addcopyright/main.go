// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Addcopyright adds copyright header to each Go file.
package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"go.astrophena.name/tools/internal/devtools"
)

var templates = map[string]string{
	".go": `// © %d Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

`,
	".star": `# © %d Ilya Mateyko. All rights reserved.
# Use of this source code is governed by the ISC
# license that can be found in the LICENSE.md file.

`,
}

var headers = map[string]string{
	".go":   `// ©`,
	".star": `# ©`,
}

var exclusions = []string{
	// Based on Go's standard library code.
	"cmd/tgfeed/internal/diff/diff.go",
	// Based on Tailscale code.
	"internal/web/debug.go",
	"internal/web/debug_test.go",
	// Based on Oscar (golang.org/x/oscar) code.
	"internal/util/rr/rr.go",
	"internal/util/rr/rr_test.go",
	// Based on LUCI code.
	"internal/devtools/starlarkdocgen/main.go",
	"internal/starlark/docgen/ast/parser.go",
	"internal/starlark/docgen/docgen.go",
	"internal/starlark/docgen/docstring/docstring.go",
	"internal/starlark/docgen/symbols/loader.go",
	"internal/starlark/docgen/symbols/symbols.go",
	"internal/starlark/interpreter/interpreter.go",
	"internal/starlark/interpreter/loaders.go",
	"internal/util/set/set.go",
}

func isExcluded(path string) bool {
	for _, ex := range exclusions {
		if strings.HasSuffix(path, ex) {
			return true
		}
	}
	return false
}

func main() {
	devtools.EnsureRoot()

	if err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || isExcluded(path) {
			return nil
		}
		ext := filepath.Ext(path)
		tmpl, ok := templates[ext]
		if !ok {
			return nil
		}
		header, ok := headers[ext]
		if !ok {
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

		if bytes.HasPrefix(content, []byte(header)) {
			return nil // Already has a copyright header
		}

		year := info.ModTime().Year()
		hdr := fmt.Sprintf(tmpl, year)

		var buf bytes.Buffer
		buf.WriteString(hdr)
		buf.Write(content)

		return os.WriteFile(path, buf.Bytes(), 0o644)
	}); err != nil {
		log.Fatal(err)
	}
}

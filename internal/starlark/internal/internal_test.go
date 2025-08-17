// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestParseDocComment(t *testing.T) {
	docFiles, err := filepath.Glob(filepath.Join("..", "*", "doc.go"))
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range docFiles {
		pkgName := filepath.Base(filepath.Dir(file))
		t.Run(pkgName, func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.TrimSpace(ParseDocComment(src))

			goldenFile := filepath.Join("testdata", pkgName+".golden")
			if *update {
				if err := os.WriteFile(goldenFile, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}

			want, err := os.ReadFile(goldenFile)
			if err != nil {
				t.Fatal(err)
			}

			testutil.AssertEqual(t, got, string(want))
		})
	}
}

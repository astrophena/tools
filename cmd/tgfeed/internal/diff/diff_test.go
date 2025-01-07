// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package diff

import (
	"bytes"
	"flag"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestDiff(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, match string) []byte {
		ar, err := txtar.ParseFile(match)
		if err != nil {
			t.Fatal(err)
		}

		if len(ar.Files) != 2 || ar.Files[0].Name != "a" || ar.Files[1].Name != "b" {
			t.Fatalf("Archive must have exactly 2 files, named 'a' and 'b'.")
		}

		a, b := ar.Files[0].Data, ar.Files[1].Data

		if strings.Contains(match, "nonl") {
			a, _ = bytes.CutSuffix(a, []byte("\n"))
		}

		return Diff("a", a, "b", b)
	}, *update)
}

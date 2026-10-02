// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"flag"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestComponents(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, match string) []byte {
		ar, err := txtar.ParseFile(match)
		if err != nil {
			t.Fatal(err)
		}
		files := make(map[string]string)
		for _, f := range ar.Files {
			files[f.Name] = string(f.Data)
		}

		var page, menu bytes.Buffer
		if err := environmentPage(strings.TrimSpace(files["stylesheet"]), files["documentation.html"]).Render(t.Context(), &page); err != nil {
			t.Fatal(err)
		}
		if err := documentationLink(strings.TrimSpace(files["sprite"])).Render(t.Context(), &menu); err != nil {
			t.Fatal(err)
		}
		return txtar.Format(&txtar.Archive{Files: []txtar.File{
			{Name: "environment.html", Data: page.Bytes()},
			{Name: "menu.html", Data: menu.Bytes()},
		}})
	}, *update)
}

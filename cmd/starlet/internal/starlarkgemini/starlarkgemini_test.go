// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package starlarkgemini

import (
	"bytes"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/tools/internal/api/google/gemini"
	"go.astrophena.name/tools/internal/starlark/interpreter"
	"go.astrophena.name/tools/internal/starlark/stdlib"
	"go.astrophena.name/tools/internal/util/rr"

	"go.starlark.net/starlark"
)

var update = flag.Bool("update", false, "update golden files in testdata")

// Updating this test:
//
//	$  GEMINI_API_KEY=... go test -update -httprecord testdata/*.httprr
//
// (notice an extra space before command to prevent recording it in shell
// history)

func TestModule(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.star", func(t *testing.T, match string) []byte {
		recFile := strings.TrimSuffix(match, filepath.Ext(match)) + ".httprr"

		rec, err := rr.Open(recFile, http.DefaultTransport)
		if err != nil {
			t.Fatal(err)
		}
		defer rec.Close()
		rec.ScrubReq(func(r *http.Request) error {
			r.Header.Del("x-goog-api-key")
			return nil
		})

		c := &gemini.Client{
			HTTPClient: rec.Client(),
		}
		if rec.Recording() {
			c.APIKey = os.Getenv("GEMINI_API_KEY")
		}

		var buf bytes.Buffer

		script, err := os.ReadFile(match)
		if err != nil {
			t.Fatal(err)
		}

		intr := &interpreter.Interpreter{
			Predeclared: starlark.StringDict{
				"gemini": Module(c),
			},
			Packages: map[string]interpreter.Loader{
				interpreter.MainPkg: interpreter.MemoryLoader(map[string]string{
					"test.star": string(script),
				}),
				interpreter.StdlibPkg: stdlib.Loader(),
			},
			Logger: func(file string, line int, message string) {
				fmt.Fprint(&buf, message)
			},
		}
		if err := intr.Init(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := intr.ExecModule(t.Context(), interpreter.MainPkg, "test.star"); err != nil {
			t.Fatal(err)
		}

		return buf.Bytes()
	}, *update)
}

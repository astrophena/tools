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
	"go.astrophena.name/tools/internal/util/rr"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

var update = flag.Bool("update", false, "update golden files in testdata")

// Updating this test:
//
//	$  GEMINI_API_KEY=... go test -httprecord testdata/*.httprr
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
		rec.Scrub(func(r *http.Request) error {
			r.Header.Del("x-goog-api-key")
			return nil
		})

		c := &gemini.Client{
			Model:      "gemini-1.5-flash",
			HTTPClient: rec.Client(),
		}
		if rec.Recording() {
			c.APIKey = os.Getenv("GEMINI_API_KEY")
		}

		var buf bytes.Buffer

		thread := &starlark.Thread{
			Name:  "test",
			Print: func(_ *starlark.Thread, msg string) { fmt.Fprintf(&buf, msg) },
		}

		predecl := starlark.StringDict{
			"gemini": Module(c),
		}

		script, err := os.ReadFile(match)
		if err != nil {
			t.Fatal(err)
		}

		if _, err = starlark.ExecFileOptions(
			&syntax.FileOptions{},
			thread,
			"test.star",
			script,
			predecl,
		); err != nil {
			t.Fatal(err)
		}

		return buf.Bytes()
	}, *update)
}

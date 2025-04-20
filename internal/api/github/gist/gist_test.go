// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package gist

import (
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.astrophena.name/tools/internal/util/rr"

	"go.astrophena.name/base/testutil"
)

func TestGet(t *testing.T) {
	rec, err := rr.Open(filepath.Join("testdata", "get.httprr"), http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	c := &Client{
		HTTPClient: rec.Client(),
	}

	// See https://gist.github.com/astrophena/f32522138ef3493c11301cd020a5fca7 for
	// the gist we are testing against.

	gist, err := c.Get(t.Context(), "f32522138ef3493c11301cd020a5fca7")
	if err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, len(gist.Files), 1)

	name, file := getFirstFile(gist)
	testutil.AssertEqual(t, name, "proxy.go")
	testutil.AssertEqual(t, strings.HasPrefix(file.Content, "// The proxy binary"), true)
}

// Updating this test:
//
//	$ GITHUB_TOKEN="$(gh auth token)" go test -httprecord testdata/update.httprr
//

func TestUpdate(t *testing.T) {
	rec, err := rr.Open(filepath.Join("testdata", "update.httprr"), http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	rec.ScrubReq(func(r *http.Request) error {
		r.Header.Del("Authorization")
		return nil
	})

	c := &Client{
		HTTPClient: rec.Client(),
		Token:      "example",
	}

	// See https://gist.github.com/42263b384c3af501bdace095928345da for
	// the gist we are testing against.
	const id = "42263b384c3af501bdace095928345da"

	if rec.Recording() {
		c.Token = os.Getenv("GITHUB_TOKEN")
	}

	gist, err := c.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, len(gist.Files), 2)
	name, file := getFirstFile(gist)
	testutil.AssertEqual(t, name, "bar.txt")
	testutil.AssertEqual(t, file.Content, "foo\n")

	gist.Files["bar.txt"] = File{Content: "foo\n"}
	gist, err = c.Update(t.Context(), id, gist)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, len(gist.Files), 2)
}

func getFirstFile(g *Gist) (name string, file File) {
	keys := slices.Sorted(maps.Keys(g.Files))
	return keys[0], g.Files[keys[0]]
}

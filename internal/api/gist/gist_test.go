package gist

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/tools/internal/request/rr"
	"go.astrophena.name/tools/internal/testutil"
)

func TestGet(t *testing.T) {
	rec, err := rr.Open(filepath.Join("testdata", "get.httprr"), http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}

	c := &Client{
		HTTPClient: rec.Client(),
	}

	// See https://gist.github.com/astrophena/98c0eeb72ee0bdba33c24d1e19780081 for
	// the gist we are testing against.

	gist, err := c.Get(context.Background(), "98c0eeb72ee0bdba33c24d1e19780081")
	if err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, len(gist.Files), 1)

	name, file := getFirstFile(gist)
	testutil.AssertEqual(t, name, "site.go")
	testutil.AssertEqual(t, strings.HasPrefix(file.Content, "package main"), true)
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
	rec.Scrub(func(r *http.Request) error {
		r.Header.Del("Authorization")
		return nil
	})

	c := &Client{
		HTTPClient: rec.Client(),
		Token:      "example",
	}

	// See https://gist.github.com/astrophena/a91d766ec189326040f0a491243a86b1 for
	// the gist we are testing against.
	const id = "a91d766ec189326040f0a491243a86b1"

	if rec.Recording() {
		c.Token = os.Getenv("GITHUB_TOKEN")
	}

	gist, err := c.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	// Initial gist contents.
	testutil.AssertEqual(t, len(gist.Files), 1)
	name, file := getFirstFile(gist)
	testutil.AssertEqual(t, name, "foo.txt")
	testutil.AssertEqual(t, file.Content, "bar\n")

	// Add a file and update our gist.
	gist.Files["bar.txt"] = File{Content: "foo\n"}
	gist, err = c.Update(context.Background(), id, gist)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, len(gist.Files), 2)
}

func getFirstFile(g *Gist) (name string, file File) {
	for n, f := range g.Files {
		name, file = n, f
	}
	return
}

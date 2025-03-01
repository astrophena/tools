// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
)

var (
	//go:embed testdata/load/gist.json
	gistJSON []byte

	//go:embed testdata/load/gist_error.json
	gistErrorJSON []byte
)

func TestLoadFromGist(t *testing.T) {
	t.Parallel()

	tm := testMux(t, nil)
	tm.gist = gistJSON
	f := testFetcher(t, tm)

	if err := f.loadFromGist(context.Background()); err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, f.errorTemplate, "test")
}

func TestLoadFromGistHandleError(t *testing.T) {
	t.Parallel()

	tm := testMux(t, map[string]http.HandlerFunc{
		"GET api.github.com/gists/test": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write(gistErrorJSON)
		},
	})
	f := testFetcher(t, tm)
	err := f.loadFromGist(context.Background())
	testutil.AssertEqual(t, err.Error(), fmt.Sprintf("GET \"https://api.github.com/gists/test\": want 200, got 404: %s", gistErrorJSON))
}

func TestFeedString(t *testing.T) {
	f := &feed{URL: atomFeedURL}
	testutil.AssertEqual(t, f.String(), fmt.Sprintf("<feed url=%q>", atomFeedURL))
}

func TestParseConfig(t *testing.T) {
	testutil.RunGolden(t, "testdata/config/*.star", func(t *testing.T, match string) []byte {
		config := readFile(t, match)

		tm := testMux(t, nil)

		ar := &txtar.Archive{
			Files: []txtar.File{
				{Name: "config.star", Data: config},
			},
		}
		tm.gist = txtarToGist(t, txtar.Format(ar))

		f := testFetcher(t, tm)
		if err := f.run(context.Background()); err != nil {
			return []byte(fmt.Sprintf("Error: %v", err))
		}

		return toJSON(t, f.feeds)
	}, *update)
}

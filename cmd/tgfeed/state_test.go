// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"fmt"
	"testing"
	"testing/fstest"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
)

func TestLoadState(t *testing.T) {
	t.Parallel()

	baseState := fstest.MapFS{
		"config.star": &fstest.MapFile{
			Data: []byte("feeds = []"),
		},
		"error.tmpl": &fstest.MapFile{
			Data: []byte("test"),
		},
	}

	tm := testMux(t, baseState, nil)
	f := testFetcher(t, tm)

	if err := f.loadState(t.Context()); err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, f.errorTemplate, "test")
}

func TestParseConfig(t *testing.T) {
	testutil.RunGolden(t, "testdata/config/*.star", func(t *testing.T, match string) []byte {
		config := readFile(t, match)

		ar := &txtar.Archive{
			Files: []txtar.File{
				{Name: "config.star", Data: config},
			},
		}

		tm := testMux(t, txtarToFS(ar), nil)
		f := testFetcher(t, tm)
		if err := f.run(t.Context()); err != nil {
			return fmt.Appendf(nil, "Error: %v", err)
		}

		return toJSON(t, f.feeds)
	}, *update)
}

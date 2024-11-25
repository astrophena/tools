// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package version

import (
	"flag"
	"os"
	"runtime/debug"
	"testing"

	"go.astrophena.name/base/testutil"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestUserAgent(t *testing.T) {
	t.Parallel()

	testutil.RunGolden(t, "testdata/useragent/*.json", func(t *testing.T, match string) []byte {
		b, err := os.ReadFile(match)
		if err != nil {
			t.Fatal(err)
		}
		bi := testutil.UnmarshalJSON[debug.BuildInfo](t, b)
		loadFunc = func() (*debug.BuildInfo, bool) {
			return &bi, true
		}
		return []byte(userAgent(loadInfo(loadFunc)))
	}, *update)
}

func TestLoadInfo(t *testing.T) {
	t.Parallel()

	testutil.RunGolden(t, "testdata/buildinfo/*.json", func(t *testing.T, match string) []byte {
		b, err := os.ReadFile(match)
		if err != nil {
			t.Fatal(err)
		}
		bi := testutil.UnmarshalJSON[debug.BuildInfo](t, b)
		biFunc := func() (*debug.BuildInfo, bool) {
			return &bi, true
		}
		return []byte(loadInfo(biFunc).String())
	}, *update)
}

func TestInfoString(t *testing.T) {
	t.Parallel()

	testutil.RunGolden(t, "testdata/info/*.json", func(t *testing.T, match string) []byte {
		b, err := os.ReadFile(match)
		if err != nil {
			t.Fatal(err)
		}
		i := testutil.UnmarshalJSON[Info](t, b)
		return []byte(i.String())
	}, *update)
}

// Copyright 2018 The LUCI Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package interpreter

import (
	"flag"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.starlark.net/starlark"
)

var update = flag.Bool("update", false, "update golden files in testdata")

func TestDeindent(t *testing.T) {
	t.Parallel()
	got := deindent(`

		a
			b
				c
			d

		e
		`)
	want := `

a
	b
		c
	d

e
`
	testutil.AssertEqual(t, got, want)
}

func TestMakeModuleKey(t *testing.T) {
	t.Parallel()
	thread := new(starlark.Thread)
	thread.SetLocal(threadModKey, ModuleKey{Package: "cur_pkg", Path: "dir/cur.star"})
	cases := map[string]struct {
		thread *starlark.Thread
		ref    string
		want   ModuleKey
		err    string
	}{
		"absolute":          {thread: thread, ref: "//some/mod", want: ModuleKey{Package: "cur_pkg", Path: "some/mod"}},
		"clean absolute":    {thread: thread, ref: "//some/mod/../blah", want: ModuleKey{Package: "cur_pkg", Path: "some/blah"}},
		"relative":          {thread: thread, ref: "some/mod", want: ModuleKey{Package: "cur_pkg", Path: "dir/some/mod"}},
		"dot relative":      {thread: thread, ref: "./mod", want: ModuleKey{Package: "cur_pkg", Path: "dir/mod"}},
		"parent relative":   {thread: thread, ref: "../mod", want: ModuleKey{Package: "cur_pkg", Path: "mod"}},
		"package absolute":  {ref: "@pkg//some/mod", want: ModuleKey{Package: "pkg", Path: "some/mod"}},
		"empty package":     {thread: thread, ref: "@//mod", err: "package alias can't be empty"},
		"malformed package": {thread: thread, ref: "@mod", err: "module path should be"},
		"outside absolute":  {thread: thread, ref: "//..", err: "outside the package root"},
		"outside relative":  {thread: thread, ref: "../../mod", err: "outside the package root"},
		"missing local":     {thread: new(starlark.Thread), ref: "//some/mod", err: "no current package name"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := MakeModuleKey(tc.thread, tc.ref)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("MakeModuleKey() error = %v, want mention %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, got, tc.want)
		})
	}
}

func TestInterpreterGolden(t *testing.T) {
	t.Parallel()
	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, match string) []byte {
		t.Parallel()
		archive, err := txtar.ParseFile(match)
		if err != nil {
			t.Fatal(err)
		}
		packages := map[string]map[string]string{
			MainPkg:   {},
			StdlibPkg: {},
			"custom":  {},
		}
		for _, file := range archive.Files {
			pkg, name := MainPkg, file.Name
			if prefix, rest, ok := strings.Cut(file.Name, "/"); ok {
				switch prefix {
				case "stdlib":
					pkg, name = StdlibPkg, rest
				case "custom":
					pkg, name = "custom", rest
				}
			}
			packages[pkg][filepath.ToSlash(name)] = string(file.Data)
		}
		result := runInterpreter(t.Context(), packages[MainPkg], packages[StdlibPkg], packages["custom"], strings.TrimSpace(string(archive.Comment)) == "custom: native")
		return []byte(result.String())
	}, *update)
}

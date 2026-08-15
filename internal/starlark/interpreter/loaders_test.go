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
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestLoaders(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "a", "module.star")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("value = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		loader  Loader
		path    string
		want    string
		wantErr error
	}{
		"filesystem":         {loader: FileSystemLoader(dir), path: "a/module.star", want: "value = 1\n"},
		"filesystem missing": {loader: FileSystemLoader(dir), path: "missing.star", wantErr: ErrNoModule},
		"filesystem outside": {loader: FileSystemLoader(dir), path: "../module.star", wantErr: errors.New("outside the package root")},
		"fs":                 {loader: FSLoader(fstest.MapFS{"module.star": {Data: []byte("value = 2\n")}}), path: "module.star", want: "value = 2\n"},
		"fs missing":         {loader: FSLoader(fstest.MapFS{}), path: "missing.star", wantErr: ErrNoModule},
		"memory":             {loader: MemoryLoader(map[string]string{"module.star": "value = 3\n"}), path: "module.star", want: "value = 3\n"},
		"memory missing":     {loader: MemoryLoader(nil), path: "missing.star", wantErr: ErrNoModule},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dict, got, err := tc.loader(tc.path)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) && (err == nil || err.Error() != tc.wantErr.Error()) {
					t.Fatalf("loader error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if dict != nil {
				t.Fatalf("loader dict = %v, want nil", dict)
			}
			if got != tc.want {
				t.Fatalf("loader source = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFSLoaderPropagatesErrors(t *testing.T) {
	t.Parallel()
	want := errors.New("read failed")
	loader := FSLoader(errorFS{err: want})
	if _, _, err := loader("module.star"); !errors.Is(err, want) {
		t.Fatalf("FSLoader error = %v, want %v", err, want)
	}
}

type errorFS struct{ err error }

func (e errorFS) Open(string) (fs.File, error) { return nil, e.err }

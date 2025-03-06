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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.starlark.net/starlark"
)

// FSLoader returns a loader that loads files from a fs.FS implementation.
func FSLoader(fsys fs.FS) Loader {
	return func(path string) (_ starlark.StringDict, src string, err error) {
		switch body, err := fs.ReadFile(fsys, path); {
		case errors.Is(err, fs.ErrNotExist):
			return nil, "", ErrNoModule
		case err != nil:
			return nil, "", err
		default:
			return nil, string(body), nil
		}
	}
}

// FileSystemLoader returns a loader that loads files from the file system.
func FileSystemLoader(root string) Loader {
	root, err := filepath.Abs(root)
	if err != nil {
		panic(err)
	}
	return func(path string) (_ starlark.StringDict, src string, err error) {
		abs := filepath.Join(root, filepath.FromSlash(path))
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return nil, "", fmt.Errorf("failed to calculate relative path: %v", err)
		}
		if strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, "", errors.New("outside the package root")
		}
		body, err := os.ReadFile(abs)
		if os.IsNotExist(err) {
			return nil, "", ErrNoModule
		}
		return nil, string(body), err
	}
}

// MemoryLoader returns a loader that loads files from the given map.
func MemoryLoader(files map[string]string) Loader {
	return func(path string) (_ starlark.StringDict, src string, err error) {
		body, ok := files[path]
		if !ok {
			return nil, "", ErrNoModule
		}
		return nil, body, nil
	}
}

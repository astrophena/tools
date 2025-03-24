// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package devtools contains common functionality for development tools.
package devtools

import (
	"os"
	"path/filepath"
)

// Must is a helper that wraps a call to a function returning (T,
// error) and panics if the error is non-nil.
func Must[T any](val T, err error) T {
	if err != nil {
		panic(err)
	}
	return val
}

// EnsureRoot checks that the current working directory is at the repository
// root and panics if it doesn't.
func EnsureRoot() {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	if _, err := os.Stat(filepath.Join(wd, ".git")); os.IsNotExist(err) {
		panic("Are you at repo root?")
	} else if err != nil {
		panic(err)
	}
}

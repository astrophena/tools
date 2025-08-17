// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Package kvcache contains a Starlark module for a simple key-value cache with time-to-live (TTL) expiration based on last access time.

This module provides two functions for using a simple key-value cache:

  - get(key: str) -> any | None: Retrieves the value associated with the
    given string key. Returns the stored value if the key exists and has
    not expired. Returns None if the key is not found or if the entry
    has expired. Accessing a key resets its TTL timer.
  - set(key: str, value: any): Stores the given value under the specified
    string key. Any existing value for the key is overwritten. Storing a
    value resets the TTL timer for that key.
*/
package kvcache

import (
	_ "embed"
	"sync"

	"go.astrophena.name/tools/internal/starlark/internal"
)

//go:embed doc.go
var doc []byte

var Documentation = sync.OnceValue(func() string {
	return internal.ParseDocComment(doc)
})

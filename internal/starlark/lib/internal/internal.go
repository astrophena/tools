// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package internal contains helpers shared by Starlark libraries.
package internal

import (
	"bufio"
	"bytes"
	"strings"
)

// ParseDocComment parses a doc comment from the provided source code.
func ParseDocComment(src []byte) string {
	s := bufio.NewScanner(bytes.NewReader(src))
	var (
		doc       string
		inComment bool
	)
	for s.Scan() {
		line := s.Text()
		if line == "/*" {
			inComment = true
			continue
		}
		if line == "*/" {
			// Comment ended, stop scanning.
			break
		}
		if inComment {
			if strings.HasPrefix(line, "Package") {
				continue
			}
			doc += line + "\n"
		}
	}
	if err := s.Err(); err != nil {
		panic(err)
	}
	return doc
}

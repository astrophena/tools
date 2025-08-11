// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"bufio"
	"bytes"
	"strings"
)

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
			if strings.HasPrefix(s.Text(), "Package") {
				continue
			}
			doc += s.Text() + "\n"
		}
	}
	if err := s.Err(); err != nil {
		panic(err)
	}
	return doc
}

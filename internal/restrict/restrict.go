// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package restrict

import (
	"context"
	"testing"

	"github.com/landlock-lsm/go-landlock/landlock"
)

// DoUnlessTesting applies the provided set of [landlock.Rule] to restrict all
// goroutines within the program, unless the program is running under 'go test'.
//
// If sandboxing fails, a log message will be generated, but the program will
// continue execution.
func DoUnlessTesting(ctx context.Context, rules ...landlock.Rule) {
	if !testing.Testing() {
		Do(ctx, rules...)
	}
}

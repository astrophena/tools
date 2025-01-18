// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:build linux && !android

package restrict

import (
	"context"

	"go.astrophena.name/tools/internal/cli"

	"github.com/landlock-lsm/go-landlock/landlock"
)

// Do applies the provided set of [landlock.Rule] to restrict all goroutines
// within the program.
//
// If sandboxing fails, a log message will be generated, but the program will
// continue execution.
func Do(ctx context.Context, rules ...landlock.Rule) {
	if err := landlock.V5.BestEffort().Restrict(rules...); err != nil {
		cli.GetEnv(ctx).Logf("Sandboxing failed: %v", err)
	}
}

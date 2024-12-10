// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:build linux && !android

package restrict

import (
	"context"

	"github.com/landlock-lsm/go-landlock/landlock"
	"go.astrophena.name/tools/internal/cli"
)

// Do restricts all goroutines of this program to set of [landlock.Rule].
func Do(ctx context.Context, rules ...landlock.Rule) {
	if err := landlock.V5.BestEffort().Restrict(rules...); err != nil {
		cli.GetEnv(ctx).Logf("Sandboxing failed: %v", err)
	}
}

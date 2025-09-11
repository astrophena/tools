// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:build linux && !android

package restrict

import (
	"context"
	"log/slog"

	"go.astrophena.name/base/logger"

	"github.com/landlock-lsm/go-landlock/landlock"
)

// Do applies the provided set of [landlock.Rule] to restrict all goroutines
// within the program.
//
// If sandboxing fails, a log message will be generated, but the program will
// continue execution.
func Do(ctx context.Context, rules ...landlock.Rule) {
	if err := landlock.V5.BestEffort().Restrict(rules...); err != nil {
		logger.Warn(ctx, "sandboxing failed", slog.Any("err", err))
	}
}

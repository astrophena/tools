// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:build linux && !android

package restrict

import (
	"context"
	"log/slog"
	"testing"

	"github.com/landlock-lsm/go-landlock/landlock"
	"go.astrophena.name/base/logger"
)

// DoUnlessTesting applies Landlock rules and logs any failure. It skips go test.
func DoUnlessTesting(ctx context.Context, rules ...landlock.Rule) {
	if testing.Testing() {
		return
	}
	if err := landlock.V5.BestEffort().Restrict(rules...); err != nil {
		logger.Warn(ctx, "sandboxing failed", slog.Any("err", err))
	}
}

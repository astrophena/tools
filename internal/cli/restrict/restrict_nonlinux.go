// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:build android || !linux

package restrict

import (
	"context"

	"github.com/landlock-lsm/go-landlock/landlock"
)

// Do is a no-op on non-Linux systems and Android.
func Do(_ context.Context, _ ...landlock.Rule) {}

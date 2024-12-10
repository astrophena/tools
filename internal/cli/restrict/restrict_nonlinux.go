//go:build android || !linux

package restrict

import (
	"context"

	"github.com/landlock-lsm/go-landlock/landlock"
)

// Do does nothing.
func Do(_ context.Context, _ ...landlock.Rule) {}

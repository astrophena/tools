//go:build linux && !android

package restrict

import (
	"context"

	"github.com/landlock-lsm/go-landlock/landlock"
	"go.astrophena.name/tools/internal/cli"
)

// Do restricts all goroutines of this program to [landlock.Rule]s.
func Do(ctx context.Context, rules ...landlock.Rule) {
	if err := landlock.V5.BestEffort().Restrict(rules...); err != nil {
		cli.GetEnv(ctx).Logf("Sandboxing failed: %v", err)
	}
}

//go:build android || !linux

package restrict

import "github.com/landlock-lsm/go-landlock/landlock"

// Do does nothing.
func Do(_ ...landlock.Rule) {}

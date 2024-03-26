package cli

import (
	"testing"
)

func TestEnsureNotStarted(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("SetArgsUsage did not panic after calling HandleStartup")
		}
	}()

	HandleStartup()
	SetArgsUsage("foo")
}

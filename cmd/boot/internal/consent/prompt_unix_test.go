// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:build unix

package consent

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	boot "go.astrophena.name/tools/cmd/boot/internal"
)

func TestAskCanceledWhileWaitingForInput(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	// Unblock the read if it fails to respond to the deadline.
	watchdog := time.AfterFunc(5*time.Second, func() { w.Close() })
	_, err = ask(ctx, &boot.Runtime{
		Stdin:  r,
		Stdout: io.Discard,
	}, "Proceed?", false)
	if !watchdog.Stop() {
		t.Fatal("prompt waited for the input pipe to close")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ask() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

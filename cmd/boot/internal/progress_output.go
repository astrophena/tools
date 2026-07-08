// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package internal

import (
	"bytes"
	"io"
	"sync"
)

type progressPrinter interface {
	Printf(format string, args ...any)
}

// progressOutput prints module output without corrupting the active progress bar.
type progressOutput struct {
	mu sync.Mutex
	pb progressPrinter

	buf bytes.Buffer
}

func newProgressOutput(pb progressPrinter) *progressOutput {
	return &progressOutput{pb: pb}
}

func (w *progressOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	written := len(p)
	for len(p) > 0 {
		idx := bytes.IndexByte(p, '\n')
		if idx == -1 {
			w.buf.Write(p)
			return written, nil
		}
		w.buf.Write(p[:idx])
		w.printLocked()
		p = p[idx+1:]
	}
	return written, nil
}

func (w *progressOutput) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.buf.Len() > 0 {
		w.printLocked()
	}
}

func (w *progressOutput) printLocked() {
	w.pb.Printf("%s", w.buf.String())
	w.buf.Reset()
}

var _ io.Writer = (*progressOutput)(nil)

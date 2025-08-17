// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// This package is adapted from gokrazy's code, see
// https://github.com/gokrazy/gokrazy/blob/main/LICENSE.

// Package logstream implements an [io.Writer] that buffers lines written to it
// in a ring buffer and allows them to be streamed through an HTTP endpoint or
// retrieved as a snapshot.
package logstream

import (
	"container/ring"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Streamer is an [io.Writer] that contains all logged lines and allows to
// stream them.
type Streamer interface {
	io.Writer
	http.Handler

	// Lines returns all logged lines.
	Lines() []string

	// Stream generates a new channel which will stream any newly logged lines.
	// Deregister the stream by calling the returned function.
	Stream() (<-chan string, func())
}

// New returns a new Streamer backed by a ring buffer of the given size.
func New(size int) Streamer {
	return &lineRingBuffer{
		size:    size,
		r:       ring.New(size),
		streams: make(map[chan string]struct{}),
	}
}

type lineRingBuffer struct {
	sync.RWMutex
	size      int
	remainder string
	r         *ring.Ring
	streams   map[chan string]struct{}
}

func (lrb *lineRingBuffer) Write(b []byte) (int, error) {
	lrb.Lock()
	defer lrb.Unlock()
	text := lrb.remainder + string(b)
	for {
		idx := strings.Index(text, "\n")
		if idx == -1 {
			break
		}

		line := text[:idx+1] // Include the newline character.
		lrb.r.Value = line
		for stream := range lrb.streams {
			select {
			case stream <- line:
			default:
				// If receiver channel is blocking, skip. This means streams will miss
				// log lines if they are full.
			}
		}
		lrb.r = lrb.r.Next()
		text = text[idx+1:]
	}
	lrb.remainder = text
	return len(b), nil
}

func (lrb *lineRingBuffer) Lines() []string {
	lrb.RLock()
	defer lrb.RUnlock()
	lines := make([]string, 0, lrb.r.Len())
	lrb.r.Do(func(x any) {
		if x != nil {
			lines = append(lines, x.(string))
		}
	})
	return lines
}

func (lrb *lineRingBuffer) Stream() (<-chan string, func()) {
	lrb.Lock()
	defer lrb.Unlock()

	stream := make(chan string, lrb.size+1)
	lrb.streams[stream] = struct{}{}

	return stream, func() {
		lrb.Lock()
		defer lrb.Unlock()

		delete(lrb.streams, stream)
		close(stream)
	}
}

func (lrb *lineRingBuffer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	evtStream := eventStreamRequested(r)
	if evtStream {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	stream, closeFunc := lrb.Stream()
	defer closeFunc()

	for {
		select {
		case line := <-stream:
			// See https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events for description
			// of server-sent events protocol.
			if evtStream {
				line = fmt.Sprintf("event: logline\ndata: %s\n", line)
			}
			fmt.Fprintln(w, line)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case <-r.Context().Done():
			// Client closed stream. Stop and release all resources immediately.
			return
		}
	}
}

func eventStreamRequested(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
}

var (
	_ Streamer     = (*lineRingBuffer)(nil)
	_ http.Handler = (*lineRingBuffer)(nil)
)

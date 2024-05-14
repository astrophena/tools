// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package httplogger provides a http.RoundTripper middleware that logs HTTP
// requests and responses.
//
// It wraps an existing http.RoundTripper and logs information about each
// request and response, including the start time, URL, method, status code (if
// available), and any errors. The logs are formatted with timestamps and
// indentation to visually represent the nesting of requests.
package httplogger

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Logf is a simple printf-like logging function.
type Logf func(format string, args ...any)

// New creates a new http.RoundTripper that logs information about HTTP requests
// and responses.
func New(t http.RoundTripper, logf Logf) http.RoundTripper {
	if logf == nil {
		logf = log.Printf
	}
	return &loggingTransport{transport: t, logf: logf}
}

type loggingTransport struct {
	transport http.RoundTripper
	mu        sync.Mutex
	logf      Logf
	active    []byte
}

func (t *loggingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.mu.Lock()
	index := len(t.active)
	start := time.Now()
	t.logf("HTTP: %s %s+ %s", timeFormat(start), t.active, r.URL)
	t.active = append(t.active, '|')
	t.mu.Unlock()

	resp, err := t.transport.RoundTrip(r)

	last := r.URL.Path
	if i := strings.LastIndex(last, "/"); i >= 0 {
		last = last[i:]
	}
	display := last
	if resp != nil {
		display += " " + resp.Status
	}
	if err != nil {
		display += " error: " + err.Error()
	}
	now := time.Now()

	t.mu.Lock()
	t.active[index] = '-'
	t.logf("HTTP: %s %s %s (%.3fs)", timeFormat(now), t.active, display, now.Sub(start).Seconds())
	t.active[index] = ' '
	n := len(t.active)
	for n%4 == 0 && n >= 4 && t.active[n-1] == ' ' && t.active[n-2] == ' ' && t.active[n-3] == ' ' && t.active[n-4] == ' ' {
		t.active = t.active[:n-4]
		n -= 4
	}
	t.mu.Unlock()

	return resp, err
}

func timeFormat(t time.Time) string {
	return t.Format("15:04:05.000")
}

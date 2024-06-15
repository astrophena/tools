package logstream

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.astrophena.name/tools/internal/testutil"
)

func TestLogStreamerLines(t *testing.T) {
	lgs, wbuf, lg := setupLogger()
	lg.Printf("test")
	lg.Printf("test2")
	testutil.AssertEqual(t, strings.Join(lgs.Lines(), "\n")+"\n", wbuf.String())
}

func TestLogStreamerStream(t *testing.T) {
	lgs, _, lg := setupLogger()

	want := []string{
		"test",
		"test2",
		"test3",
	}
	var got []string
	var mu sync.Mutex

	var wg sync.WaitGroup

	stream, closeFunc := lgs.Stream()
	wg.Add(1)
	go func() {
		for line := range stream {
			mu.Lock()
			got = append(got, line)
			mu.Unlock()
		}
		wg.Done()
	}()

	for _, line := range want {
		lg.Printf(line)
	}

	closeFunc()
	wg.Wait()
	testutil.AssertEqual(t, want, got)
}

func TestLogStreamerHTTPHandler(t *testing.T) {
	test := func(eventStream bool) func(t *testing.T) {
		return func(t *testing.T) {
			lgs, wbuf, lg := setupLogger()

			s := httptest.NewServer(lgs)
			defer s.Close()
			c := s.Client()

			req, err := http.NewRequest(http.MethodGet, s.URL, nil)
			if err != nil {
				t.Fatalf("creating request: %v", err)
			}
			if eventStream {
				req.Header.Set("Accept", "text/event-stream")
			}

			res, err := c.Do(req)
			if err != nil {
				t.Fatalf("making request: %v", err)
			}

			want := []string{
				"test",
				"test2",
				"test3",
			}
			var mu sync.Mutex
			go func() {
				for _, line := range want {
					lg.Printf(line)
				}
			}()

			wantRead := len(want)
			if eventStream {
				wantRead = wantRead * 3 // each log line is represented by two lines + newline
			}

			var got []string

			r := bufio.NewReader(res.Body)
			for i := 0; i < wantRead; i++ {
				line, err := r.ReadString('\n')
				if err != nil {
					break
				}
				mu.Lock()
				got = append(got, line)
				mu.Unlock()
			}
			res.Body.Close()

			if eventStream {
				var wantFormatted strings.Builder
				for _, line := range want {
					line := line
					io.WriteString(&wantFormatted, "event: logline\n")
					io.WriteString(&wantFormatted, "data: "+line+"\n\n")
				}
				testutil.AssertEqual(t, strings.Join(got, ""), wantFormatted.String())
				return
			}
			testutil.AssertEqual(t, strings.Join(got, ""), wbuf.String())
		}
	}

	t.Run("plain text", test(false))
	t.Run("event stream", test(true))
}

func TestEventStreamRequested(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/debug/log", nil)

	if eventStreamRequested(req) {
		t.Errorf("eventStreamRequested returns true for non event-stream request")
	}

	req.Header.Set("Accept", "text/event-stream")
	if !eventStreamRequested(req) {
		t.Errorf("eventStreamRequested returns false for event-stream request")
	}
}

func setupLogger() (lgs Streamer, wbuf *syncBuffer, lg *log.Logger) {
	lgs = New(10)
	wbuf = &syncBuffer{
		buf: new(bytes.Buffer),
	}
	mw := io.MultiWriter(lgs, wbuf)
	return lgs, wbuf, log.New(mw, "", 0)
}

// syncBuffer is a synchronized version of bytes.Buffer.
type syncBuffer struct {
	mu  sync.RWMutex
	buf *bytes.Buffer
}

func (sb *syncBuffer) Write(p []byte) (n int, err error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

func (sb *syncBuffer) String() string {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.buf.String()
}

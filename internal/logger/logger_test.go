package logger

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
)

func TestLogfWriter(t *testing.T) {
	t.Parallel()

	var (
		logged  bool
		message string
	)
	logf := func(format string, args ...any) {
		logged = true
		message = fmt.Sprintf(format, args...)
	}
	Logf(logf).Write([]byte("hello"))
	testutil.AssertEqual(t, logged, true)
	testutil.AssertEqual(t, message, "hello")
}

func TestStreamer(t *testing.T) {
	t.Parallel()

	s := NewStreamer(5)

	testLines := []string{
		"Line 1",
		"Line 2",
		"Line 3",
		"Line 4",
		"Line 5",
		"Line 6", // This should push out "Line 1" due to buffer size.
	}

	for _, line := range testLines {
		_, err := s.Write([]byte(line + "\n"))
		if err != nil {
			t.Fatalf("Failed to write line: %v", err)
		}
	}

	lines := s.Lines()
	if len(lines) != 5 {
		t.Errorf("Expected 5 lines, got %d", len(lines))
	}
	if lines[0] != "Line 2\n" || lines[4] != "Line 6\n" {
		t.Errorf("Unexpected lines content: %v", lines)
	}

	// Test streaming.
	stream, close := s.Stream()
	defer close()

	go func() {
		_, err := s.Write([]byte("New line\n"))
		if err != nil {
			t.Errorf("Failed to write new line: %v", err)
		}
	}()

	select {
	case line := <-stream:
		if line != "New line\n" {
			t.Errorf("Expected 'New line\\n', got '%s'", line)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for streamed line")
	}

	// Test HTTP handler.
	req := httptest.NewRequest("GET", "/log", nil)
	req.Header.Set("Accept", "text/event-stream")
	w := httptest.NewRecorder()

	go func() {
		time.Sleep(100 * time.Millisecond)
		_, err := s.Write([]byte("HTTP line\n"))
		if err != nil {
			t.Errorf("Failed to write HTTP line: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}()

	ctx, cancel := context.WithTimeout(req.Context(), 500*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	s.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK, got %v", resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Expected Content-Type text/event-stream, got %s", ct)
	}

	body := w.Body.String()
	expectedLine := "event: logline\ndata: HTTP line\n"
	if !strings.Contains(body, expectedLine) {
		t.Errorf("Expected body to contain '%s', got '%s'", expectedLine, body)
	}
}

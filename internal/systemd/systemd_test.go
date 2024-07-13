package systemd_test

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"go.astrophena.name/tools/internal/systemd"
)

type testLogger struct {
	messages []string
}

func (tl *testLogger) logf(format string, args ...any) {
	tl.messages = append(tl.messages, fmt.Sprintf(format, args...))
}

func TestNotify(t *testing.T) {
	tl := &testLogger{}
	socketPath := filepath.Join(t.TempDir(), "notify.sock")

	l, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: socketPath, Net: "unixgram"})
	if err != nil {
		t.Fatalf("Failed to listen on unixgram socket: %v", err)
	}
	defer l.Close()

	t.Setenv("NOTIFY_SOCKET", socketPath)

	systemd.Notify(tl.logf, systemd.Ready)

	buf := make([]byte, 512)
	n, _, err := l.ReadFromUnix(buf)
	if err != nil {
		t.Fatalf("Failed to read from unixgram socket: %v", err)
	}

	expected := "READY=1"
	if string(buf[:n]) != expected {
		t.Errorf("Expected to receive %s, but got %s", expected, string(buf[:n]))
	}
}

func TestWatchdogLoop(t *testing.T) {
	tl := &testLogger{}
	socketPath := filepath.Join(t.TempDir(), "watchdog.sock")

	l, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: socketPath, Net: "unixgram"})
	if err != nil {
		t.Fatalf("Failed to listen on unixgram socket: %v", err)
	}
	defer l.Close()

	t.Setenv("NOTIFY_SOCKET", socketPath)
	t.Setenv("WATCHDOG_USEC", "250000") // 0.25 second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go systemd.WatchdogLoop(ctx, tl.logf)

	buf := make([]byte, 512)
	n, _, err := l.ReadFromUnix(buf)
	if err != nil {
		t.Fatalf("Failed to read from unixgram socket: %v", err)
	}

	expected := "WATCHDOG=1"
	if string(buf[:n]) != expected {
		t.Errorf("Expected to receive %s, but got %s", expected, string(buf[:n]))
	}

	cancel()

	// Clear the buffer and try to read again; there should be no more messages.
	l.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
	n, _, err = l.ReadFromUnix(buf)
	if err == nil && n > 0 {
		t.Errorf("Expected no more messages, but got %s", string(buf[:n]))
	}
}

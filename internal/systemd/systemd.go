// Package systemd enables applications to signal readiness and update watchdog
// timestamp to systemd.
package systemd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"go.astrophena.name/tools/internal/logger"
)

// State defines a sd-notify protocol state.
// See https://www.freedesktop.org/software/systemd/man/sd_notify.html.
type State string

const (
	// Ready tells the service manager that service startup is
	// finished, or the service finished loading its configuration.
	// See https://www.freedesktop.org/software/systemd/man/sd_notify.html#READY=1.
	Ready State = "READY=1"

	// Watchdog tells the service manager to update the watchdog timestamp.
	// See https://www.freedesktop.org/software/systemd/man/sd_notify.html#WATCHDOG=1.
	Watchdog State = "WATCHDOG=1"
)

// Notify sends a message to systemd using the sd_notify protocol. If there are
// an error, it will be logged to logf.
func Notify(logf logger.Logf, state State) {
	addr := &net.UnixAddr{
		Net:  "unixgram",
		Name: os.Getenv("NOTIFY_SOCKET"),
	}

	if addr.Name == "" {
		// We're not running under systemd (NOTIFY_SOCKET is not set).
		return
	}

	conn, err := net.DialUnix(addr.Net, nil, addr)
	if err != nil {
		logf("systemd: failed when notifying: %v", err)
		return
	}
	defer conn.Close()

	if _, err = conn.Write([]byte(state)); err != nil {
		logf("systemd: failed when notifying: %v", err)
		return
	}
}

// WatchdogLoop periodically updates systemd watchdog timestamp. It should run in
// a separate goroutine and can be stopped by canceling the provided [context.Context].
// If there are any errors, they will be logged to logf.
func WatchdogLoop(ctx context.Context, logf logger.Logf) {
	if os.Getenv("WATCHDOG_USEC") == "" {
		return
	}

	interval, err := watchdogInterval()
	if err != nil {
		logf("%v", err)
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			Notify(logf, Watchdog)
		case <-ctx.Done():
			return
		}
	}
}

func watchdogInterval() (time.Duration, error) {
	s, err := strconv.Atoi(os.Getenv("WATCHDOG_USEC"))
	if err != nil {
		return 0, fmt.Errorf("systemd: error converting WATCHDOG_USEC: %v", err)
	}

	if s <= 0 {
		return 0, errors.New("systemd: error WATCHDOG_USEC must be a positive number")
	}

	return time.Duration(s) * time.Microsecond, nil
}

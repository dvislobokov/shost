// Package sdnotify integrates shost with the systemd notification protocol
// (sd_notify): readiness for Type=notify units, stopping status, and
// watchdog keep-alives. Everything is a no-op when the process does not
// run under systemd (NOTIFY_SOCKET unset), so the same binary works as a
// unit, in a container and from a terminal:
//
//	host := sdnotify.Bind(shost.New().
//		AddService(...)).
//		MustBuild()
//
// Standard library only.
package sdnotify

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/dvislobokov/shost"
)

// ErrNotAvailable is returned by Notify when the process does not run
// under systemd (NOTIFY_SOCKET is unset).
var ErrNotAvailable = errors.New("sdnotify: NOTIFY_SOCKET is not set")

// Available reports whether the systemd notification socket is present.
func Available() bool { return os.Getenv("NOTIFY_SOCKET") != "" }

// Notify sends a raw sd_notify state string (e.g. "READY=1") to the
// notification socket. Most callers should use Ready, Stopping, Status or
// Watchdog instead.
func Notify(state string) error {
	socket := os.Getenv("NOTIFY_SOCKET")
	if socket == "" {
		return ErrNotAvailable
	}
	if socket[0] == '@' { // abstract socket namespace
		socket = "\x00" + socket[1:]
	}
	conn, err := net.Dial("unixgram", socket)
	if err != nil {
		return fmt.Errorf("sdnotify: dial %s: %w", os.Getenv("NOTIFY_SOCKET"), err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(state)); err != nil {
		return fmt.Errorf("sdnotify: write: %w", err)
	}
	return nil
}

// Ready reports readiness (READY=1) — for Type=notify units, systemd
// considers the service started only after this.
func Ready() error { return Notify("READY=1") }

// Stopping reports that shutdown has begun (STOPPING=1).
func Stopping() error { return Notify("STOPPING=1") }

// Status sets the human-readable status line shown by systemctl status.
func Status(msg string) error { return Notify("STATUS=" + msg) }

// Watchdog sends a watchdog keep-alive (WATCHDOG=1).
func Watchdog() error { return Notify("WATCHDOG=1") }

// WatchdogEnabled reports whether systemd expects watchdog keep-alives
// from this process (WatchdogSec= in the unit) and the configured
// interval. Pings must arrive more often than the interval; half of it is
// the customary rate.
func WatchdogEnabled() (time.Duration, bool) {
	usec := os.Getenv("WATCHDOG_USEC")
	if usec == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(usec, 10, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	if pid := os.Getenv("WATCHDOG_PID"); pid != "" && pid != strconv.Itoa(os.Getpid()) {
		return 0, false
	}
	return time.Duration(v) * time.Microsecond, true
}

// Bind wires systemd notifications into the host lifecycle: READY=1 on
// OnStarted, STOPPING=1 on OnStopping, and — when the unit has
// WatchdogSec= — a "sdnotify-watchdog" service pinging at half the
// configured interval. Returns the builder for chaining; does nothing
// outside systemd.
func Bind(b *shost.Builder) *shost.Builder {
	if !Available() {
		return b
	}
	b.OnStarted(func() { _ = Ready() }).
		OnStopping(func() { _ = Stopping() })
	if interval, ok := WatchdogEnabled(); ok {
		b.AddService(watchdogService(interval / 2))
	}
	return b
}

func watchdogService(interval time.Duration) shost.Service {
	return shost.ServiceFunc("sdnotify-watchdog", func(ctx context.Context) error {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				_ = Watchdog()
			}
		}
	})
}

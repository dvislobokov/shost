package sdnotify_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dvislobokov/shost"
	"github.com/dvislobokov/shost/sdnotify"
	"github.com/dvislobokov/shost/shosttest"
)

// listenNotify creates a unixgram socket standing in for systemd's and
// points NOTIFY_SOCKET at it. Skips on platforms without unixgram
// support (Windows).
func listenNotify(t *testing.T) <-chan string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "notify.sock")
	conn, err := net.ListenPacket("unixgram", path)
	if err != nil {
		t.Skipf("unixgram not supported: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	t.Setenv("NOTIFY_SOCKET", path)

	messages := make(chan string, 16)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				close(messages)
				return
			}
			messages <- string(buf[:n])
		}
	}()
	return messages
}

func recv(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(3 * time.Second):
		t.Fatal("no sd_notify message received")
		return ""
	}
}

func TestNotifyWithoutSocket(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	os.Unsetenv("NOTIFY_SOCKET")
	if sdnotify.Available() {
		t.Fatal("Available must be false without NOTIFY_SOCKET")
	}
	if err := sdnotify.Ready(); !errors.Is(err, sdnotify.ErrNotAvailable) {
		t.Fatalf("expected ErrNotAvailable, got: %v", err)
	}
}

func TestNotifySendsStates(t *testing.T) {
	messages := listenNotify(t)

	if err := sdnotify.Ready(); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, messages); got != "READY=1" {
		t.Fatalf("got %q, want READY=1", got)
	}
	if err := sdnotify.Status("collecting"); err != nil {
		t.Fatal(err)
	}
	if got := recv(t, messages); got != "STATUS=collecting" {
		t.Fatalf("got %q", got)
	}
}

func TestWatchdogEnabled(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "30000000")
	t.Setenv("WATCHDOG_PID", strconv.Itoa(os.Getpid()))
	d, ok := sdnotify.WatchdogEnabled()
	if !ok || d != 30*time.Second {
		t.Fatalf("got (%v, %v), want (30s, true)", d, ok)
	}

	t.Setenv("WATCHDOG_PID", "1") // meant for another process
	if _, ok := sdnotify.WatchdogEnabled(); ok {
		t.Fatal("watchdog must be disabled for a foreign WATCHDOG_PID")
	}

	t.Setenv("WATCHDOG_USEC", "garbage")
	t.Setenv("WATCHDOG_PID", strconv.Itoa(os.Getpid()))
	if _, ok := sdnotify.WatchdogEnabled(); ok {
		t.Fatal("watchdog must be disabled for invalid WATCHDOG_USEC")
	}
}

func TestBindLifecycleAndWatchdog(t *testing.T) {
	messages := listenNotify(t)
	t.Setenv("WATCHDOG_USEC", "100000") // 100ms → pings every 50ms
	t.Setenv("WATCHDOG_PID", strconv.Itoa(os.Getpid()))

	h := shosttest.Start(t, sdnotify.Bind(shost.New().
		AddService(shost.ServiceFunc("a", func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}))))

	if got := recv(t, messages); got != "READY=1" {
		t.Fatalf("got %q, want READY=1", got)
	}
	if got := recv(t, messages); got != "WATCHDOG=1" {
		t.Fatalf("got %q, want WATCHDOG=1", got)
	}

	if err := h.Stop(); err != nil {
		t.Fatalf("expected clean shutdown, got: %v", err)
	}
	// STOPPING=1 must have been sent; watchdog pings may interleave.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case m := <-messages:
			if m == "STOPPING=1" {
				return
			}
		case <-deadline:
			t.Fatal("STOPPING=1 not received")
		}
	}
}

func TestBindOutsideSystemdIsNoop(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	os.Unsetenv("NOTIFY_SOCKET")
	b := shost.New().AddService(shost.ServiceFunc("a", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}))
	h := shosttest.Start(t, sdnotify.Bind(b))
	if err := h.Stop(); err != nil {
		t.Fatalf("expected clean shutdown, got: %v", err)
	}
}

func TestUnit(t *testing.T) {
	unit := sdnotify.Unit(sdnotify.UnitConfig{
		Description:    "My agent",
		ExecStart:      "/usr/local/bin/agent --config /etc/agent.yaml",
		User:           "agent",
		Environment:    []string{"APP_ENVIRONMENT=Production"},
		WatchdogSec:    30 * time.Second,
		TimeoutStopSec: 40 * time.Second,
	})
	for _, want := range []string{
		"Type=notify",
		"Description=My agent",
		"ExecStart=/usr/local/bin/agent --config /etc/agent.yaml",
		"User=agent",
		"Environment=APP_ENVIRONMENT=Production",
		"WatchdogSec=30",
		"TimeoutStopSec=40",
		"Restart=on-failure",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q:\n%s", want, unit)
		}
	}
}

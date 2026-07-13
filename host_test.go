package shost_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dvislobokov/shost"
)

// blockingService is a well-behaved Service: Start blocks until ctx is
// canceled, Stop returns immediately. It records lifecycle order.
type blockingService struct {
	name         string
	events       *eventLog
	startErr     error // returned from Start immediately, without waiting for ctx
	stopErr      error // returned from Stop
	startPanic   bool
	ignoreCancel bool // Start never returns, even after cancel
}

func (s *blockingService) Name() string { return s.name }

func (s *blockingService) Start(ctx context.Context) error {
	if s.startPanic {
		panic("boom in " + s.name)
	}
	if s.startErr != nil {
		return s.startErr
	}
	s.events.add("start:" + s.name)
	if s.ignoreCancel {
		select {} // simulates a stuck service
	}
	<-ctx.Done()
	return ctx.Err()
}

func (s *blockingService) Stop(ctx context.Context) error {
	s.events.add("stop:" + s.name)
	return s.stopErr
}

type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) add(e string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *eventLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

// runHost runs the host in a goroutine and returns a wait func.
func runHost(t *testing.T, h *shost.Host, ctx context.Context) func() error {
	t.Helper()
	res := make(chan error, 1)
	go func() { res <- h.RunContext(ctx) }()
	return func() error {
		select {
		case err := <-res:
			return err
		case <-time.After(5 * time.Second):
			t.Fatal("host did not stop in time")
			return nil
		}
	}
}

func TestGracefulShutdownReverseOrder(t *testing.T) {
	events := &eventLog{}
	h := shost.New().
		AddService(&blockingService{name: "a", events: events}).
		AddService(&blockingService{name: "b", events: events}).
		AddService(&blockingService{name: "c", events: events}).
		MustBuild()

	ctx, cancel := context.WithCancel(context.Background())
	wait := runHost(t, h, ctx)

	waitFor(t, events, "start:a", "start:b", "start:c")
	cancel()

	if err := wait(); err != nil {
		t.Fatalf("expected clean shutdown, got: %v", err)
	}

	// Start goroutines are launched in order but run concurrently, so only
	// the stop order is strictly guaranteed: reverse registration order.
	got := events.list()
	stops := got[len(got)-3:]
	want := []string{"stop:c", "stop:b", "stop:a"}
	if strings.Join(stops, ",") != strings.Join(want, ",") {
		t.Fatalf("wrong stop order:\n got %v\nwant %v", stops, want)
	}
}

func TestShutdownMethodStopsHost(t *testing.T) {
	events := &eventLog{}
	h := shost.New().
		AddService(&blockingService{name: "a", events: events}).
		MustBuild()

	wait := runHost(t, h, context.Background())
	waitFor(t, events, "start:a")

	h.Shutdown()
	h.Shutdown() // idempotent

	if err := wait(); err != nil {
		t.Fatalf("expected clean shutdown, got: %v", err)
	}
}

func TestServiceFailureStopsHost(t *testing.T) {
	events := &eventLog{}
	boom := errors.New("boom")
	h := shost.New().
		AddService(&blockingService{name: "a", events: events}).
		AddService(&blockingService{name: "b", events: events, startErr: boom}).
		MustBuild()

	err := runHost(t, h, context.Background())()
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "service b failed") {
		t.Fatalf("error should name the failed service, got: %v", err)
	}
	// The healthy service must still be stopped.
	if !contains(events.list(), "stop:a") {
		t.Fatalf("service a was not stopped, events: %v", events.list())
	}
}

func TestServiceNilExitStopsHost(t *testing.T) {
	events := &eventLog{}
	h := shost.New().
		AddService(shost.ServiceFunc("oneshot", func(ctx context.Context) error {
			return nil // exits immediately without an error
		})).
		AddService(&blockingService{name: "a", events: events}).
		MustBuild()

	err := runHost(t, h, context.Background())()
	if err == nil || !strings.Contains(err.Error(), "exited unexpectedly") {
		t.Fatalf("expected unexpected-exit error, got: %v", err)
	}
}

func TestPanicInStartIsRecovered(t *testing.T) {
	events := &eventLog{}
	h := shost.New().
		AddService(&blockingService{name: "a", events: events}).
		AddService(&blockingService{name: "p", events: events, startPanic: true}).
		MustBuild()

	err := runHost(t, h, context.Background())()
	if err == nil || !strings.Contains(err.Error(), "panic in Start: boom in p") {
		t.Fatalf("expected recovered panic error, got: %v", err)
	}
	if !contains(events.list(), "stop:a") {
		t.Fatalf("service a was not stopped after panic, events: %v", events.list())
	}
}

func TestShutdownTimeoutOnStuckService(t *testing.T) {
	events := &eventLog{}
	h := shost.New().
		WithShutdownTimeout(100 * time.Millisecond).
		AddService(&blockingService{name: "a", events: events}).
		AddService(&blockingService{name: "stuck", events: events, ignoreCancel: true}).
		MustBuild()

	ctx, cancel := context.WithCancel(context.Background())
	wait := runHost(t, h, ctx)
	waitFor(t, events, "start:a", "start:stuck")
	cancel()

	err := wait()
	if err == nil || !strings.Contains(err.Error(), "service stuck did not stop within shutdown timeout") {
		t.Fatalf("expected shutdown timeout error, got: %v", err)
	}
}

func TestStopErrorIsReported(t *testing.T) {
	events := &eventLog{}
	stopBoom := errors.New("stop boom")
	h := shost.New().
		AddService(&blockingService{name: "a", events: events, stopErr: stopBoom}).
		MustBuild()

	ctx, cancel := context.WithCancel(context.Background())
	wait := runHost(t, h, ctx)
	waitFor(t, events, "start:a")
	cancel()

	if err := wait(); !errors.Is(err, stopBoom) {
		t.Fatalf("expected stop error, got: %v", err)
	}
}

func waitFor(t *testing.T, events *eventLog, want ...string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got := events.list()
		all := true
		for _, w := range want {
			if !contains(got, w) {
				all = false
				break
			}
		}
		if all {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for events %v, got %v", want, events.list())
}

func contains(list []string, v string) bool {
	for _, e := range list {
		if e == v {
			return true
		}
	}
	return false
}

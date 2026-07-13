package shost_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvislobokov/shost"
)

// readyService implements shost.Readier: it closes the ready channel
// readyAfter after Start begins, or never if readyAfter is zero.
type readyService struct {
	name       string
	events     *eventLog
	ready      chan struct{}
	readyAfter time.Duration
}

func newReadyService(name string, events *eventLog, readyAfter time.Duration) *readyService {
	return &readyService{name: name, events: events, ready: make(chan struct{}), readyAfter: readyAfter}
}

func (s *readyService) Name() string           { return s.name }
func (s *readyService) Ready() <-chan struct{} { return s.ready }

func (s *readyService) Start(ctx context.Context) error {
	s.events.add("start:" + s.name)
	if s.readyAfter > 0 {
		time.Sleep(s.readyAfter)
		s.events.add("ready:" + s.name)
		close(s.ready)
	}
	<-ctx.Done()
	return ctx.Err()
}

func (s *readyService) Stop(ctx context.Context) error {
	s.events.add("stop:" + s.name)
	return nil
}

// flakyService fails failures times, then blocks until canceled.
type flakyService struct {
	name     string
	events   *eventLog
	failures int32
	runs     atomic.Int32
}

func (s *flakyService) Name() string { return s.name }

func (s *flakyService) Start(ctx context.Context) error {
	run := s.runs.Add(1)
	s.events.add("run:" + s.name)
	if run <= s.failures {
		return errors.New("flaky failure")
	}
	<-ctx.Done()
	return ctx.Err()
}

func (s *flakyService) Stop(ctx context.Context) error { return nil }

func TestRestartRecoversFailingService(t *testing.T) {
	events := &eventLog{}
	flaky := &flakyService{name: "f", events: events, failures: 2}
	h := shost.New().
		AddService(flaky, shost.WithRestart(shost.RestartPolicy{
			InitialDelay: 5 * time.Millisecond,
			MaxDelay:     10 * time.Millisecond,
		})).
		MustBuild()

	ctx, cancel := context.WithCancel(context.Background())
	wait := runHost(t, h, ctx)

	// Two failing runs, then the third succeeds and blocks.
	deadline := time.Now().Add(3 * time.Second)
	for flaky.runs.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if flaky.runs.Load() < 3 {
		t.Fatalf("service was not restarted, runs=%d", flaky.runs.Load())
	}
	cancel()

	if err := wait(); err != nil {
		t.Fatalf("expected clean shutdown after recovery, got: %v", err)
	}
	if got := flaky.runs.Load(); got != 3 {
		t.Fatalf("expected 3 runs, got %d", got)
	}
}

func TestRestartGivesUpAfterMaxAttempts(t *testing.T) {
	events := &eventLog{}
	flaky := &flakyService{name: "f", events: events, failures: 100}
	h := shost.New().
		AddService(flaky, shost.WithRestart(shost.RestartPolicy{
			MaxAttempts:  2,
			InitialDelay: time.Millisecond,
			MaxDelay:     2 * time.Millisecond,
		})).
		MustBuild()

	err := runHost(t, h, context.Background())()
	if err == nil || !strings.Contains(err.Error(), "service f failed") {
		t.Fatalf("expected failure after exhausted restarts, got: %v", err)
	}
	// Initial run + 2 restarts.
	if got := flaky.runs.Load(); got != 3 {
		t.Fatalf("expected 3 runs, got %d", got)
	}
}

func TestShutdownDuringRestartWaitIsClean(t *testing.T) {
	events := &eventLog{}
	flaky := &flakyService{name: "f", events: events, failures: 100}
	h := shost.New().
		AddService(flaky, shost.WithRestart(shost.RestartPolicy{
			InitialDelay: 10 * time.Second, // host shuts down mid-wait
		})).
		MustBuild()

	wait := runHost(t, h, context.Background())
	waitFor(t, events, "run:f")
	time.Sleep(20 * time.Millisecond) // let the supervisor enter the restart wait
	h.Shutdown()

	if err := wait(); err != nil {
		t.Fatalf("expected clean shutdown during restart wait, got: %v", err)
	}
}

func TestReadinessGatesNextService(t *testing.T) {
	events := &eventLog{}
	h := shost.New().
		AddService(newReadyService("a", events, 30*time.Millisecond)).
		AddService(&blockingService{name: "b", events: events}).
		MustBuild()

	ctx, cancel := context.WithCancel(context.Background())
	wait := runHost(t, h, ctx)
	waitFor(t, events, "start:b")
	cancel()
	if err := wait(); err != nil {
		t.Fatalf("expected clean shutdown, got: %v", err)
	}

	got := events.list()
	if indexOf(got, "ready:a") > indexOf(got, "start:b") {
		t.Fatalf("service b started before a was ready: %v", got)
	}
}

func TestStartTimeoutOnNeverReadyService(t *testing.T) {
	events := &eventLog{}
	h := shost.New().
		WithStartTimeout(50 * time.Millisecond).
		AddService(newReadyService("slow", events, 0 /* never ready */)).
		AddService(&blockingService{name: "b", events: events}).
		MustBuild()

	err := runHost(t, h, context.Background())()
	if err == nil || !strings.Contains(err.Error(), "service slow not ready within start timeout") {
		t.Fatalf("expected start timeout error, got: %v", err)
	}
	got := events.list()
	if contains(got, "start:b") {
		t.Fatalf("service b must not start after timeout: %v", got)
	}
	if !contains(got, "stop:slow") {
		t.Fatalf("service slow was not stopped: %v", got)
	}
}

func TestLifecycleHooksOrder(t *testing.T) {
	events := &eventLog{}
	h := shost.New().
		AddService(&blockingService{name: "a", events: events}).
		OnStarted(func() { events.add("hook:started") }).
		OnStopping(func() { events.add("hook:stopping") }).
		OnStopped(func() { events.add("hook:stopped") }).
		MustBuild()

	ctx, cancel := context.WithCancel(context.Background())
	wait := runHost(t, h, ctx)
	waitFor(t, events, "hook:started")
	cancel()
	if err := wait(); err != nil {
		t.Fatalf("expected clean shutdown, got: %v", err)
	}

	got := events.list()
	order := []string{"hook:started", "hook:stopping", "stop:a", "hook:stopped"}
	last := -1
	for _, e := range order {
		idx := indexOf(got, e)
		if idx <= last {
			t.Fatalf("wrong hook order, want %v within %v", order, got)
		}
		last = idx
	}
}

func TestHookPanicIsRecovered(t *testing.T) {
	events := &eventLog{}
	h := shost.New().
		AddService(&blockingService{name: "a", events: events}).
		OnStarted(func() { panic("hook boom") }).
		OnStarted(func() { events.add("hook:second") }).
		MustBuild()

	ctx, cancel := context.WithCancel(context.Background())
	wait := runHost(t, h, ctx)
	waitFor(t, events, "hook:second") // the panic must not skip later hooks
	cancel()
	if err := wait(); err != nil {
		t.Fatalf("hook panic must not fail the host, got: %v", err)
	}
}

func TestBuildRejectsInvalidRestartPolicy(t *testing.T) {
	_, err := shost.New().
		AddService(nopService("w"), shost.WithRestart(shost.RestartPolicy{Factor: 0.5})).
		Build()
	if err == nil || !strings.Contains(err.Error(), "restart factor must be >= 1") {
		t.Fatalf("expected restart policy error, got: %v", err)
	}
}

func TestBuildRejectsNilHook(t *testing.T) {
	_, err := shost.New().OnStarted(nil).Build()
	if err == nil || !strings.Contains(err.Error(), "OnStarted called with nil hook") {
		t.Fatalf("expected nil hook error, got: %v", err)
	}
}

func indexOf(list []string, v string) int {
	for i, e := range list {
		if e == v {
			return i
		}
	}
	return -1
}

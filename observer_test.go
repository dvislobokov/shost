package shost_test

import (
	"context"
	"testing"
	"time"

	"github.com/dvislobokov/shost"
)

func TestObserverReceivesLifecycleEvents(t *testing.T) {
	events := &eventLog{}
	var hostErr error
	obs := shost.Observer{
		HostStarted:    func() { events.add("obs:host-started") },
		HostStopped:    func(err error) { hostErr = err; events.add("obs:host-stopped") },
		ServiceStarted: func(name string) { events.add("obs:started:" + name) },
		ServiceStopped: func(name string, elapsed time.Duration, err error) {
			events.add("obs:stopped:" + name)
		},
	}
	h := shost.New().
		AddService(&blockingService{name: "a", events: events}).
		WithObserver(obs).
		MustBuild()

	ctx, cancel := context.WithCancel(context.Background())
	wait := runHost(t, h, ctx)
	waitFor(t, events, "obs:host-started")
	cancel()
	if err := wait(); err != nil {
		t.Fatalf("expected clean shutdown, got: %v", err)
	}
	if hostErr != nil {
		t.Fatalf("HostStopped must receive nil on clean shutdown, got: %v", hostErr)
	}
	for _, want := range []string{"obs:started:a", "obs:host-started", "obs:stopped:a", "obs:host-stopped"} {
		if !contains(events.list(), want) {
			t.Fatalf("missing observer event %q in %v", want, events.list())
		}
	}
}

func TestObserverSeesFailureAndRestart(t *testing.T) {
	events := &eventLog{}
	var restarts, failures int
	obs := shost.Observer{
		ServiceRestarting: func(name string, attempt int, delay time.Duration, err error) { restarts++ },
		ServiceFailed:     func(name string, err error) { failures++ },
	}
	flaky := &flakyService{name: "f", events: events, failures: 100}
	h := shost.New().
		WithObserver(obs).
		AddService(flaky, shost.WithRestart(shost.RestartPolicy{
			MaxAttempts:  1,
			InitialDelay: time.Millisecond,
		})).
		MustBuild()

	err := runHost(t, h, context.Background())()
	if err == nil {
		t.Fatal("expected failure")
	}
	if restarts != 1 || failures != 1 {
		t.Fatalf("expected 1 restart and 1 failure observed, got restarts=%d failures=%d", restarts, failures)
	}
}

func TestObserverPanicDoesNotBreakHost(t *testing.T) {
	events := &eventLog{}
	h := shost.New().
		WithObserver(shost.Observer{HostStarted: func() { panic("observer boom") }}).
		AddService(&blockingService{name: "a", events: events}).
		MustBuild()

	ctx, cancel := context.WithCancel(context.Background())
	wait := runHost(t, h, ctx)
	waitFor(t, events, "start:a")
	cancel()
	if err := wait(); err != nil {
		t.Fatalf("observer panic must not fail the host, got: %v", err)
	}
}

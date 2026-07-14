package shost_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dvislobokov/shost"
)

func TestStartupTasksRunInOrderBeforeServices(t *testing.T) {
	events := &eventLog{}
	h := shost.New().
		AddStartupTask("migrate", func(ctx context.Context) error {
			events.add("task:migrate")
			return nil
		}).
		AddStartupTask("warmup", func(ctx context.Context) error {
			events.add("task:warmup")
			return nil
		}).
		AddService(&blockingService{name: "a", events: events}).
		MustBuild()

	ctx, cancel := context.WithCancel(context.Background())
	wait := runHost(t, h, ctx)
	waitFor(t, events, "start:a")
	cancel()

	if err := wait(); err != nil {
		t.Fatalf("expected clean shutdown, got: %v", err)
	}
	got := events.list()
	want := []string{"task:migrate", "task:warmup", "start:a", "stop:a"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("wrong order:\n got %v\nwant %v", got, want)
	}
}

func TestStartupTaskFailurePreventsStart(t *testing.T) {
	events := &eventLog{}
	boom := errors.New("boom")
	h := shost.New().
		AddStartupTask("migrate", func(ctx context.Context) error { return boom }).
		AddStartupTask("never", func(ctx context.Context) error {
			events.add("task:never")
			return nil
		}).
		AddService(&blockingService{name: "a", events: events}).
		MustBuild()

	err := runHost(t, h, context.Background())()
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "startup task migrate") {
		t.Fatalf("error should name the failed task, got: %v", err)
	}
	if len(events.list()) != 0 {
		t.Fatalf("no task or service should have run, events: %v", events.list())
	}
}

func TestStartupTaskPanicIsRecovered(t *testing.T) {
	h := shost.New().
		AddStartupTask("bad", func(ctx context.Context) error { panic("task boom") }).
		AddService(shost.ServiceFunc("a", func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})).
		MustBuild()

	err := runHost(t, h, context.Background())()
	if err == nil || !strings.Contains(err.Error(), "panic: task boom") {
		t.Fatalf("expected recovered panic error, got: %v", err)
	}
}

func TestStartupTaskCanceledByShutdown(t *testing.T) {
	events := &eventLog{}
	entered := make(chan struct{})
	h := shost.New().
		AddStartupTask("slow", func(ctx context.Context) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		}).
		AddService(&blockingService{name: "a", events: events}).
		MustBuild()

	res := make(chan error, 1)
	go func() { res <- h.RunContext(context.Background()) }()

	<-entered
	h.Shutdown()

	select {
	case err := <-res:
		// Interrupted startup is a clean exit, not a failure.
		if err != nil {
			t.Fatalf("expected clean exit, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("host did not stop in time")
	}
	if contains(events.list(), "start:a") {
		t.Fatalf("service must not start after canceled task, events: %v", events.list())
	}
}

func TestStartupTaskValidation(t *testing.T) {
	if _, err := shost.New().AddStartupTask("", func(ctx context.Context) error { return nil }).Build(); err == nil {
		t.Fatal("expected error for empty task name")
	}
	if _, err := shost.New().AddStartupTask("x", nil).Build(); err == nil {
		t.Fatal("expected error for nil task")
	}
}

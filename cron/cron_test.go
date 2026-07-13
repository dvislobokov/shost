package cron_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvislobokov/shost"
	"github.com/dvislobokov/shost/cron"
)

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

func TestJobRunsPeriodically(t *testing.T) {
	var runs atomic.Int32
	svc := cron.Every("tick", 10*time.Millisecond, func(ctx context.Context) error {
		runs.Add(1)
		return nil
	})
	h := shost.New().AddService(svc).MustBuild()

	ctx, cancel := context.WithCancel(context.Background())
	wait := runHost(t, h, ctx)

	deadline := time.Now().Add(3 * time.Second)
	for runs.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := wait(); err != nil {
		t.Fatalf("expected clean shutdown, got: %v", err)
	}
	if runs.Load() < 3 {
		t.Fatalf("expected at least 3 runs, got %d", runs.Load())
	}
}

func TestRunImmediately(t *testing.T) {
	var runs atomic.Int32
	svc := cron.Every("tick", time.Hour, func(ctx context.Context) error {
		runs.Add(1)
		return nil
	}, cron.RunImmediately())
	h := shost.New().AddService(svc).MustBuild()

	ctx, cancel := context.WithCancel(context.Background())
	wait := runHost(t, h, ctx)

	deadline := time.Now().Add(3 * time.Second)
	for runs.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := wait(); err != nil {
		t.Fatalf("expected clean shutdown, got: %v", err)
	}
	if runs.Load() != 1 {
		t.Fatalf("expected exactly 1 immediate run, got %d", runs.Load())
	}
}

func TestErrorContinuesScheduleAndReachesHandler(t *testing.T) {
	var runs atomic.Int32
	var handled atomic.Int32
	svc := cron.Every("flaky", 10*time.Millisecond, func(ctx context.Context) error {
		runs.Add(1)
		return errors.New("job failed")
	}, cron.WithErrorHandler(func(err error) { handled.Add(1) }))
	h := shost.New().AddService(svc).MustBuild()

	ctx, cancel := context.WithCancel(context.Background())
	wait := runHost(t, h, ctx)

	deadline := time.Now().Add(3 * time.Second)
	for runs.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := wait(); err != nil {
		t.Fatalf("job errors must not stop the host by default, got: %v", err)
	}
	if runs.Load() < 2 || handled.Load() < 2 {
		t.Fatalf("expected repeated runs and handled errors, runs=%d handled=%d", runs.Load(), handled.Load())
	}
}

func TestStopOnErrorStopsHost(t *testing.T) {
	boom := errors.New("boom")
	svc := cron.Every("fatal", time.Hour, func(ctx context.Context) error {
		return boom
	}, cron.RunImmediately(), cron.StopOnError())
	h := shost.New().AddService(svc).MustBuild()

	err := runHost(t, h, context.Background())()
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom to stop host, got: %v", err)
	}
}

func TestPanicInJobIsRecovered(t *testing.T) {
	var handled atomic.Value
	svc := cron.Every("panicky", time.Hour, func(ctx context.Context) error {
		panic("job boom")
	}, cron.RunImmediately(), cron.WithErrorHandler(func(err error) { handled.Store(err.Error()) }))
	h := shost.New().AddService(svc).MustBuild()

	ctx, cancel := context.WithCancel(context.Background())
	wait := runHost(t, h, ctx)

	deadline := time.Now().Add(3 * time.Second)
	for handled.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := wait(); err != nil {
		t.Fatalf("panic must not stop the host without StopOnError, got: %v", err)
	}
	got, _ := handled.Load().(string)
	if !strings.Contains(got, "panic in job panicky: job boom") {
		t.Fatalf("expected recovered panic in handler, got: %q", got)
	}
}

func TestEveryPanicsOnInvalidConfig(t *testing.T) {
	for name, fn := range map[string]func(){
		"empty name":    func() { cron.Every("", time.Second, func(ctx context.Context) error { return nil }) },
		"zero interval": func() { cron.Every("x", 0, func(ctx context.Context) error { return nil }) },
		"nil job":       func() { cron.Every("x", time.Second, nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s: expected panic", name)
				}
			}()
			fn()
		}()
	}
}

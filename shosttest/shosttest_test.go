package shosttest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dvislobokov/shost"
	"github.com/dvislobokov/shost/shosttest"
)

func blocking(name string) shost.Service {
	return shost.ServiceFunc(name, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
}

func TestStartAndStop(t *testing.T) {
	rec := shosttest.NewRecorder()
	h := shosttest.Start(t, shost.New().
		WithObserver(rec.Observer()).
		AddService(blocking("a")).
		AddService(blocking("b")))

	if !rec.Has(shosttest.HostStarted, "") {
		t.Fatalf("HostStarted not recorded, events: %v", rec.Events())
	}
	if !rec.Has(shosttest.ServiceStarted, "a") || !rec.Has(shosttest.ServiceStarted, "b") {
		t.Fatalf("ServiceStarted not recorded for both services, events: %v", rec.Events())
	}

	if err := h.Stop(); err != nil {
		t.Fatalf("expected clean shutdown, got: %v", err)
	}
	if !rec.Has(shosttest.ServiceStopped, "a") || !rec.Has(shosttest.HostStopped, "") {
		t.Fatalf("stop events not recorded, events: %v", rec.Events())
	}
}

func TestCleanupStopsHostAutomatically(t *testing.T) {
	var h *shosttest.Host
	t.Run("inner", func(t *testing.T) {
		h = shosttest.Start(t, shost.New().AddService(blocking("a")))
	})
	// The subtest's cleanup has run; the host must already be stopped.
	select {
	case err := <-waitErr(h):
		if err != nil {
			t.Fatalf("expected clean shutdown from cleanup, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup did not stop the host")
	}
}

func waitErr(h *shosttest.Host) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- h.Wait() }()
	return ch
}

func TestWaitReturnsServiceFailure(t *testing.T) {
	boom := errors.New("boom")
	fail := make(chan struct{})
	rec := shosttest.NewRecorder()
	h := shosttest.Start(t, shost.New().
		WithObserver(rec.Observer()).
		AddService(shost.ServiceFunc("flaky", func(ctx context.Context) error {
			select {
			case <-fail:
				return boom
			case <-ctx.Done():
				return ctx.Err()
			}
		})))

	close(fail)
	if err := h.Wait(); !errors.Is(err, boom) {
		t.Fatalf("expected boom, got: %v", err)
	}
	// Wait is repeatable.
	if err := h.Wait(); !errors.Is(err, boom) {
		t.Fatalf("expected boom on second Wait, got: %v", err)
	}
	if !rec.Has(shosttest.ServiceFailed, "flaky") {
		t.Fatalf("ServiceFailed not recorded, events: %v", rec.Events())
	}
}

func TestRecorderWaitFor(t *testing.T) {
	exit := make(chan struct{}, 1)
	exit <- struct{}{} // first run exits immediately, triggering a restart
	rec := shosttest.NewRecorder()
	h := shosttest.Start(t, shost.New().
		WithObserver(rec.Observer()).
		AddService(shost.ServiceFunc("w", func(ctx context.Context) error {
			select {
			case <-exit:
				return errors.New("transient")
			case <-ctx.Done():
				return ctx.Err()
			}
		}), shost.WithRestart(shost.RestartPolicy{InitialDelay: time.Millisecond})))

	if !rec.WaitFor(shosttest.ServiceRestarting, "w", 3*time.Second) {
		t.Fatalf("ServiceRestarting not recorded, events: %v", rec.Events())
	}
	_ = h.Stop()
}

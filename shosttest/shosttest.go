// Package shosttest provides test helpers for shost applications: running
// a host inside a test with automatic cleanup, and recording lifecycle
// events for assertions.
//
//	rec := shosttest.NewRecorder()
//	h := shosttest.Start(t,
//		shost.New().
//			WithObserver(rec.Observer()).
//			AddService(httpsvc.New(":0", mux)))
//	// ... exercise the running services ...
//	if err := h.Stop(); err != nil { t.Fatal(err) }
package shosttest

import (
	"context"
	"testing"
	"time"

	"github.com/dvislobokov/shost"
)

// DefaultTimeout bounds Start and Wait when the test does not finish
// naturally — a safety net against a hung host deadlocking the test suite.
const DefaultTimeout = 10 * time.Second

// Host is a shost.Host running inside a test.
type Host struct {
	t    testing.TB
	host *shost.Host
	done chan error
}

// Start builds the host from the builder, runs it in a goroutine and
// blocks until the host reports started (all services launched and ready).
// Build errors, startup failures and a startup hanging beyond
// DefaultTimeout fail the test. A cleanup stopping the host is registered
// automatically; call Stop explicitly to assert on the shutdown error.
func Start(t testing.TB, b *shost.Builder) *Host {
	t.Helper()

	started := make(chan struct{})
	host, err := b.OnStarted(func() { close(started) }).Build()
	if err != nil {
		t.Fatalf("shosttest: build failed: %v", err)
	}

	h := &Host{t: t, host: host, done: make(chan error, 1)}
	go func() { h.done <- host.RunContext(context.Background()) }()

	select {
	case <-started:
	case err := <-h.done:
		h.done <- err // keep it available for Wait
		t.Fatalf("shosttest: host stopped during startup: %v", err)
	case <-time.After(DefaultTimeout):
		t.Fatalf("shosttest: host did not start within %v", DefaultTimeout)
	}

	t.Cleanup(func() {
		host.Shutdown()
		select {
		case err := <-h.done:
			h.done <- err
		case <-time.After(DefaultTimeout):
			t.Errorf("shosttest: host did not stop within %v", DefaultTimeout)
		}
	})
	return h
}

// Host returns the underlying shost.Host.
func (h *Host) Host() *shost.Host { return h.host }

// Shutdown triggers graceful shutdown without waiting; pair with Wait.
func (h *Host) Shutdown() { h.host.Shutdown() }

// Wait blocks until Run returns and reports its error. It fails the test
// after DefaultTimeout.
func (h *Host) Wait() error {
	h.t.Helper()
	select {
	case err := <-h.done:
		h.done <- err // subsequent Wait/cleanup calls see the same result
		return err
	case <-time.After(DefaultTimeout):
		h.t.Fatalf("shosttest: host did not stop within %v", DefaultTimeout)
		return nil
	}
}

// Stop is Shutdown followed by Wait.
func (h *Host) Stop() error {
	h.t.Helper()
	h.host.Shutdown()
	return h.Wait()
}

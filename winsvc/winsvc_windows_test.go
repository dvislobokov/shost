//go:build windows

package winsvc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvislobokov/shost"
	"golang.org/x/sys/windows/svc"
)

func blocking(name string) shost.Service {
	return shost.ServiceFunc(name, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
}

// newTestHandler mirrors the service path of Run: it wires the started
// channel and builds the host.
func newTestHandler(t *testing.T, b *shost.Builder) *handler {
	t.Helper()
	started := make(chan struct{})
	host, err := b.OnStarted(func() { close(started) }).Build()
	if err != nil {
		t.Fatal(err)
	}
	return &handler{host: host, started: started}
}

type executeResult struct {
	svcSpecific bool
	exitCode    uint32
}

func runExecute(h *handler, r chan svc.ChangeRequest, s chan svc.Status) <-chan executeResult {
	done := make(chan executeResult, 1)
	go func() {
		ok, code := h.Execute(nil, r, s)
		done <- executeResult{ok, code}
	}()
	return done
}

// waitState drains status updates until the wanted state appears.
func waitState(t *testing.T, s <-chan svc.Status, want svc.State) svc.Status {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case st := <-s:
			if st.State == want {
				return st
			}
		case <-deadline:
			t.Fatalf("state %d not reported in time", want)
		}
	}
}

func TestExecuteLifecycle(t *testing.T) {
	var reloads atomic.Int32
	h := newTestHandler(t, shost.New().
		OnReload(func() { reloads.Add(1) }).
		AddService(blocking("a")))

	r := make(chan svc.ChangeRequest)
	s := make(chan svc.Status, 64)
	done := runExecute(h, r, s)

	waitState(t, s, svc.StartPending)
	running := waitState(t, s, svc.Running)
	wantAccepts := svc.AcceptStop | svc.AcceptShutdown | svc.AcceptParamChange
	if running.Accepts != wantAccepts {
		t.Fatalf("wrong accepts: %v", running.Accepts)
	}

	// Interrogate echoes the current status back.
	cur := svc.Status{State: svc.Running, Accepts: wantAccepts}
	r <- svc.ChangeRequest{Cmd: svc.Interrogate, CurrentStatus: cur}
	if got := waitState(t, s, svc.Running); got != cur {
		t.Fatalf("interrogate echo mismatch: %+v", got)
	}

	// ParamChange → Host.Reload.
	r <- svc.ChangeRequest{Cmd: svc.ParamChange}
	deadline := time.After(3 * time.Second)
	for reloads.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("reload hook not invoked")
		case <-time.After(5 * time.Millisecond):
		}
	}

	// Stop → graceful shutdown, Stopped, exit code 0.
	r <- svc.ChangeRequest{Cmd: svc.Stop}
	waitState(t, s, svc.StopPending)
	waitState(t, s, svc.Stopped)
	res := <-done
	if res.svcSpecific || res.exitCode != 0 {
		t.Fatalf("expected clean exit, got: %+v", res)
	}
	if h.runErr != nil {
		t.Fatalf("expected nil runErr, got: %v", h.runErr)
	}
}

func TestExecuteStopPendingCheckpoints(t *testing.T) {
	release := make(chan struct{})
	slow := shost.ServiceFunc("slow", func(ctx context.Context) error {
		<-ctx.Done()
		<-release // simulates a slow drain
		return ctx.Err()
	})
	h := newTestHandler(t, shost.New().
		WithShutdownTimeout(10*time.Second).
		AddService(slow))

	r := make(chan svc.ChangeRequest)
	s := make(chan svc.Status, 64)
	done := runExecute(h, r, s)
	waitState(t, s, svc.Running)

	r <- svc.ChangeRequest{Cmd: svc.Stop}
	first := waitState(t, s, svc.StopPending)
	if first.CheckPoint == 0 || first.WaitHint == 0 {
		t.Fatalf("checkpoint/waithint not set: %+v", first)
	}
	second := waitState(t, s, svc.StopPending) // ticker update while draining
	if second.CheckPoint <= first.CheckPoint {
		t.Fatalf("checkpoint did not advance: %d -> %d", first.CheckPoint, second.CheckPoint)
	}

	close(release)
	waitState(t, s, svc.Stopped)
	if res := <-done; res.exitCode != 0 {
		t.Fatalf("expected clean exit, got: %+v", res)
	}
}

func TestExecuteServiceFailure(t *testing.T) {
	boom := errors.New("boom")
	fail := make(chan struct{})
	h := newTestHandler(t, shost.New().
		AddService(shost.ServiceFunc("flaky", func(ctx context.Context) error {
			select {
			case <-fail:
				return boom
			case <-ctx.Done():
				return ctx.Err()
			}
		})))

	r := make(chan svc.ChangeRequest)
	s := make(chan svc.Status, 64)
	done := runExecute(h, r, s)
	waitState(t, s, svc.Running)

	close(fail)
	waitState(t, s, svc.Stopped)
	res := <-done
	if !res.svcSpecific || res.exitCode != 1 {
		t.Fatalf("expected service-specific exit 1, got: %+v", res)
	}
	if !errors.Is(h.runErr, boom) {
		t.Fatalf("runErr should carry the failure, got: %v", h.runErr)
	}
}

func TestExecuteStartupFailure(t *testing.T) {
	boom := errors.New("migrate failed")
	h := newTestHandler(t, shost.New().
		AddStartupTask("migrate", func(ctx context.Context) error { return boom }).
		AddService(blocking("a")))

	r := make(chan svc.ChangeRequest)
	s := make(chan svc.Status, 64)
	done := runExecute(h, r, s)

	waitState(t, s, svc.Stopped)
	res := <-done
	if !res.svcSpecific || res.exitCode != 1 {
		t.Fatalf("expected service-specific exit 1, got: %+v", res)
	}
}

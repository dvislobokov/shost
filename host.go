package shost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"
	"time"
)

// Host owns the lifecycle of its registered services: it starts them in
// registration order, waits for a shutdown trigger (OS signal, Shutdown
// call, context cancellation, or a service exiting on its own) and then
// stops everything in reverse order within the shutdown timeout.
type Host struct {
	services        []Service
	log             Logger
	shutdownTimeout time.Duration

	shutdownOnce sync.Once
	shutdownCh   chan struct{}
}

// Run starts all services and blocks until SIGINT/SIGTERM or Shutdown.
// It returns nil on a clean shutdown, or the joined errors otherwise.
func (h *Host) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return h.RunContext(ctx)
}

// RunContext is Run with a caller-provided shutdown trigger: cancellation
// of ctx initiates graceful shutdown. No OS signals are installed.
func (h *Host) RunContext(ctx context.Context) error {
	h.log.Information("host starting {ServiceCount} services", len(h.services))

	// Independent of ctx: per-service contexts are canceled one by one,
	// in reverse order, during shutdown — not all at once when ctx fires.
	baseCtx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()

	cancels := make([]context.CancelFunc, len(h.services))
	done := make([]chan error, len(h.services))
	exits := make(chan serviceExit, len(h.services))

	for i, svc := range h.services {
		svcCtx, cancel := context.WithCancel(baseCtx)
		cancels[i] = cancel
		done[i] = make(chan error, 1)
		go func(i int, svc Service) {
			err := safeStart(svc, svcCtx)
			done[i] <- err
			exits <- serviceExit{index: i, err: err}
		}(i, svc)
		h.log.Information("service {Service} started", svc.Name())
	}
	h.log.Information("host started")

	var errs []error
	causeIndex := -1

	select {
	case <-ctx.Done():
		h.log.Information("shutdown signal received")
	case <-h.shutdownCh:
		h.log.Information("shutdown requested")
	case exit := <-exits:
		causeIndex = exit.index
		name := h.services[exit.index].Name()
		if exit.err != nil {
			err := fmt.Errorf("shost: service %s failed: %w", name, exit.err)
			h.log.Error(exit.err, "service {Service} failed, stopping host", name)
			errs = append(errs, err)
		} else {
			err := fmt.Errorf("shost: service %s exited unexpectedly", name)
			h.log.Error(err, "service {Service} exited unexpectedly, stopping host", name)
			errs = append(errs, err)
		}
	}

	errs = append(errs, h.stopAll(cancels, done, causeIndex)...)
	err := errors.Join(errs...)
	if err != nil {
		h.log.Error(err, "host stopped with errors")
	} else {
		h.log.Information("host stopped")
	}
	return err
}

// Shutdown triggers graceful shutdown programmatically. Safe to call from
// any goroutine, any number of times; it does not wait for Run to return.
func (h *Host) Shutdown() {
	h.shutdownOnce.Do(func() { close(h.shutdownCh) })
}

type serviceExit struct {
	index int
	err   error
}

// stopAll stops services in reverse registration order under the shared
// shutdown deadline. causeIndex marks a service whose Start error was
// already reported as the shutdown cause.
func (h *Host) stopAll(cancels []context.CancelFunc, done []chan error, causeIndex int) []error {
	stopCtx, cancel := context.WithTimeout(context.Background(), h.shutdownTimeout)
	defer cancel()

	var errs []error
	for i := len(h.services) - 1; i >= 0; i-- {
		svc := h.services[i]
		name := svc.Name()
		h.log.Debug("service {Service} stopping", name)
		began := time.Now()
		cancels[i]()

		// Stop may itself misbehave, so it runs in a goroutine and is
		// abandoned on deadline rather than hanging the host.
		stopRes := make(chan error, 1)
		go func() { stopRes <- safeStop(svc, stopCtx) }()

		stopped := true
		select {
		case err := <-stopRes:
			if err != nil {
				errs = append(errs, fmt.Errorf("shost: stopping service %s: %w", name, err))
				h.log.Error(err, "service {Service} Stop returned error", name)
			}
		case <-stopCtx.Done():
			stopped = false
		}

		if stopped {
			select {
			case err := <-done[i]:
				if err != nil && i != causeIndex && !errors.Is(err, context.Canceled) {
					errs = append(errs, fmt.Errorf("shost: service %s failed during shutdown: %w", name, err))
					h.log.Error(err, "service {Service} failed during shutdown", name)
				}
			case <-stopCtx.Done():
				stopped = false
			}
		}

		if !stopped {
			err := fmt.Errorf("shost: service %s did not stop within shutdown timeout", name)
			errs = append(errs, err)
			h.log.Warning("service {Service} did not stop within shutdown timeout", name)
			continue
		}
		h.log.Information("service {Service} stopped in {Elapsed}", name, time.Since(began))
	}
	return errs
}

func safeStart(s Service, ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in Start: %v\n%s", r, debug.Stack())
		}
	}()
	return s.Start(ctx)
}

func safeStop(s Service, ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in Stop: %v\n%s", r, debug.Stack())
		}
	}()
	return s.Stop(ctx)
}

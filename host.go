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
	services        []registration
	tasks           []startupTask
	log             Logger
	environment     Environment
	shutdownTimeout time.Duration
	startTimeout    time.Duration
	onStarted       []func()
	onStopping      []func()
	onStopped       []func()
	observers       []Observer

	shutdownOnce sync.Once
	shutdownCh   chan struct{} // closed by Shutdown()
	stoppingCh   chan struct{} // closed when shutdown begins; stops supervisors from restarting
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
	h.log.Information("host starting {ServiceCount} services in {Environment}", len(h.services), string(h.environment))

	// Independent of ctx: per-service contexts are canceled one by one,
	// in reverse order, during shutdown — not all at once when ctx fires.
	baseCtx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()

	cancels := make([]context.CancelFunc, len(h.services))
	done := make([]chan error, len(h.services))
	exits := make(chan serviceExit, len(h.services))

	var startTimeoutCh <-chan time.Time
	if h.startTimeout > 0 {
		timer := time.NewTimer(h.startTimeout)
		defer timer.Stop()
		startTimeoutCh = timer.C
	}

	var errs []error
	causeIndex := -1
	launched := 0
	startupAborted := false

	if len(h.tasks) > 0 {
		aborted, err := h.runStartupTasks(ctx)
		if err != nil {
			errs = append(errs, err)
		}
		startupAborted = aborted || err != nil
	}

launch:
	for i, reg := range h.services {
		if startupAborted {
			break
		}
		svcCtx, cancel := context.WithCancel(baseCtx)
		cancels[i] = cancel
		done[i] = make(chan error, 1)
		go h.supervise(i, reg, svcCtx, done[i], exits)
		launched++
		h.log.Information("service {Service} started", reg.svc.Name())
		h.observe(func(o Observer) {
			if o.ServiceStarted != nil {
				o.ServiceStarted(reg.svc.Name())
			}
		})

		r, ok := reg.svc.(Readier)
		if !ok {
			continue
		}
		h.log.Debug("waiting for service {Service} readiness", reg.svc.Name())
		select {
		case <-r.Ready():
			h.log.Information("service {Service} ready", reg.svc.Name())
			h.observe(func(o Observer) {
				if o.ServiceReady != nil {
					o.ServiceReady(reg.svc.Name())
				}
			})
		case exit := <-exits:
			causeIndex = exit.index
			errs = append(errs, h.exitError(exit))
			startupAborted = true
			break launch
		case <-startTimeoutCh:
			err := fmt.Errorf("shost: service %s not ready within start timeout %v", reg.svc.Name(), h.startTimeout)
			h.log.Error(err, "service {Service} not ready within start timeout, stopping host", reg.svc.Name())
			errs = append(errs, err)
			startupAborted = true
			break launch
		case <-ctx.Done():
			h.log.Information("shutdown signal received during startup")
			startupAborted = true
			break launch
		case <-h.shutdownCh:
			h.log.Information("shutdown requested during startup")
			startupAborted = true
			break launch
		}
	}

	if !startupAborted {
		h.runHooks("OnStarted", h.onStarted)
		h.observe(func(o Observer) {
			if o.HostStarted != nil {
				o.HostStarted()
			}
		})
		h.log.Information("host started")

		select {
		case <-ctx.Done():
			h.log.Information("shutdown signal received")
		case <-h.shutdownCh:
			h.log.Information("shutdown requested")
		case exit := <-exits:
			causeIndex = exit.index
			errs = append(errs, h.exitError(exit))
		}
	}

	close(h.stoppingCh)
	h.runHooks("OnStopping", h.onStopping)
	errs = append(errs, h.stopAll(cancels, done, launched, causeIndex)...)
	h.runHooks("OnStopped", h.onStopped)

	err := errors.Join(errs...)
	if err != nil {
		h.log.Error(err, "host stopped with errors")
	} else {
		h.log.Information("host stopped")
	}
	h.observe(func(o Observer) {
		if o.HostStopped != nil {
			o.HostStopped(err)
		}
	})
	return err
}

// Environment returns the host environment (Production by default).
func (h *Host) Environment() Environment { return h.environment }

// Shutdown triggers graceful shutdown programmatically. Safe to call from
// any goroutine, any number of times; it does not wait for Run to return.
func (h *Host) Shutdown() {
	h.shutdownOnce.Do(func() { close(h.shutdownCh) })
}

// runStartupTasks runs the registered startup tasks sequentially. It
// returns aborted=true when shutdown was requested mid-startup (a clean
// exit, no error), and a non-nil error when a task failed or panicked.
func (h *Host) runStartupTasks(ctx context.Context) (aborted bool, err error) {
	// The task context also reacts to programmatic Shutdown, which does
	// not cancel ctx by itself.
	taskCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-h.shutdownCh:
			cancel()
		case <-taskCtx.Done():
		}
	}()

	for _, t := range h.tasks {
		select {
		case <-taskCtx.Done():
			h.log.Information("shutdown requested during startup tasks")
			return true, nil
		default:
		}
		h.log.Information("startup task {Task} running", t.name)
		began := time.Now()
		taskErr := safeTask(t.fn, taskCtx)
		if taskErr != nil {
			if errors.Is(taskErr, context.Canceled) && taskCtx.Err() != nil {
				h.log.Information("startup task {Task} canceled by shutdown", t.name)
				return true, nil
			}
			h.log.Error(taskErr, "startup task {Task} failed, host will not start", t.name)
			return false, fmt.Errorf("shost: startup task %s: %w", t.name, taskErr)
		}
		h.log.Information("startup task {Task} completed in {Elapsed}", t.name, time.Since(began))
	}
	return false, nil
}

type serviceExit struct {
	index int
	err   error
}

func (h *Host) exitError(exit serviceExit) error {
	name := h.services[exit.index].svc.Name()
	h.observe(func(o Observer) {
		if o.ServiceFailed != nil {
			o.ServiceFailed(name, exit.err)
		}
	})
	if exit.err != nil {
		h.log.Error(exit.err, "service {Service} failed, stopping host", name)
		return fmt.Errorf("shost: service %s failed: %w", name, exit.err)
	}
	err := fmt.Errorf("shost: service %s exited unexpectedly", name)
	h.log.Error(err, "service {Service} exited unexpectedly, stopping host", name)
	return err
}

// supervise runs the service's Start, restarting it per the registration's
// RestartPolicy. It sends the final Start error to done exactly once, and
// reports to exits only when the exit should stop the host.
func (h *Host) supervise(i int, reg registration, ctx context.Context, done chan<- error, exits chan<- serviceExit) {
	name := reg.svc.Name()
	pol := reg.restart
	attempts := 0
	var delay time.Duration
	if pol != nil {
		delay = pol.InitialDelay
	}
	for {
		began := time.Now()
		err := safeStart(reg.svc, ctx)

		// Shutdown in progress: report the final result, never restart.
		// stopAll filters context.Canceled as a graceful exit.
		select {
		case <-ctx.Done():
			done <- err
			return
		case <-h.stoppingCh:
			done <- err
			return
		default:
		}

		if pol == nil {
			done <- err
			exits <- serviceExit{index: i, err: err}
			return
		}

		if time.Since(began) >= pol.ResetAfter {
			attempts = 0
			delay = pol.InitialDelay
		}
		attempts++
		if pol.MaxAttempts > 0 && attempts > pol.MaxAttempts {
			h.log.Error(err, "service {Service} exhausted {MaxAttempts} restart attempts, stopping host", name, pol.MaxAttempts)
			done <- err
			exits <- serviceExit{index: i, err: err}
			return
		}
		if err != nil {
			h.log.Warning("service {Service} exited with {Error}, restart attempt {Attempt} in {Delay}", name, err.Error(), attempts, delay)
		} else {
			h.log.Warning("service {Service} exited, restart attempt {Attempt} in {Delay}", name, attempts, delay)
		}
		h.observe(func(o Observer) {
			if o.ServiceRestarting != nil {
				o.ServiceRestarting(name, attempts, delay, err)
			}
		})

		select {
		case <-ctx.Done():
			done <- nil // prior failure was already handled by the policy
			return
		case <-h.stoppingCh:
			done <- nil
			return
		case <-time.After(delay):
		}

		delay = time.Duration(float64(delay) * pol.Factor)
		if delay > pol.MaxDelay {
			delay = pol.MaxDelay
		}
		h.log.Information("service {Service} restarting", name)
	}
}

// stopAll stops the launched services in reverse registration order under
// the shared shutdown deadline. causeIndex marks a service whose Start
// error was already reported as the shutdown cause.
func (h *Host) stopAll(cancels []context.CancelFunc, done []chan error, launched, causeIndex int) []error {
	stopCtx, cancel := context.WithTimeout(context.Background(), h.shutdownTimeout)
	defer cancel()

	var errs []error
	for i := launched - 1; i >= 0; i-- {
		svc := h.services[i].svc
		name := svc.Name()
		h.log.Debug("service {Service} stopping", name)
		began := time.Now()
		cancels[i]()

		// Stop may itself misbehave, so it runs in a goroutine and is
		// abandoned on deadline rather than hanging the host.
		stopRes := make(chan error, 1)
		go func() { stopRes <- safeStop(svc, stopCtx) }()

		stopped := true
		var svcErr error
		select {
		case err := <-stopRes:
			if err != nil {
				svcErr = err
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
					if svcErr == nil {
						svcErr = err
					}
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
			h.observe(func(o Observer) {
				if o.ServiceStopped != nil {
					o.ServiceStopped(name, time.Since(began), err)
				}
			})
			continue
		}
		h.log.Information("service {Service} stopped in {Elapsed}", name, time.Since(began))
		h.observe(func(o Observer) {
			if o.ServiceStopped != nil {
				o.ServiceStopped(name, time.Since(began), svcErr)
			}
		})
	}
	return errs
}

func (h *Host) runHooks(name string, hooks []func()) {
	for _, fn := range hooks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					h.log.Error(fmt.Errorf("panic: %v", r), "panic in {Hook} hook", name)
				}
			}()
			fn()
		}()
	}
}

func safeStart(s Service, ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in Start: %v\n%s", r, debug.Stack())
		}
	}()
	return s.Start(ctx)
}

func safeTask(fn func(ctx context.Context) error, ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	return fn(ctx)
}

func safeStop(s Service, ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in Stop: %v\n%s", r, debug.Stack())
		}
	}()
	return s.Stop(ctx)
}

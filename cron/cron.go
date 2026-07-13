// Package cron provides periodic-job services for shost — the analog of a
// timed BackgroundService. Runs never overlap: the next tick fires only
// after the previous run completes (ticks arriving mid-run are dropped by
// time.Ticker).
package cron

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"
)

// Job is a single run of a periodic task. The passed ctx is canceled when
// the host shuts down; long jobs should respect it.
type Job func(ctx context.Context) error

// Service runs a Job on a fixed interval as a shost.Service.
type Service struct {
	name        string
	interval    time.Duration
	job         Job
	immediate   bool
	stopOnError bool
	onError     func(error)
}

// Option customizes a Service.
type Option func(*Service)

// RunImmediately runs the job once at startup, before the first tick.
func RunImmediately() Option {
	return func(s *Service) { s.immediate = true }
}

// StopOnError makes a failed run stop the service (and therefore the host,
// unless the service is registered with shost.WithRestart). By default a
// failed run is passed to the error handler and the schedule continues.
func StopOnError() Option {
	return func(s *Service) { s.stopOnError = true }
}

// WithErrorHandler receives errors (including recovered panics) of failed
// runs. Without it failed runs are silently skipped, so wiring a handler
// that logs is strongly recommended.
func WithErrorHandler(fn func(error)) Option {
	return func(s *Service) { s.onError = fn }
}

// Every creates a periodic service running job on the given interval.
// It panics on invalid arguments — configuration is a programmer error.
func Every(name string, interval time.Duration, job Job, opts ...Option) *Service {
	if name == "" {
		panic("cron: name must not be empty")
	}
	if interval <= 0 {
		panic(fmt.Sprintf("cron: interval must be positive, got %v", interval))
	}
	if job == nil {
		panic("cron: job must not be nil")
	}
	s := &Service{name: name, interval: interval, job: job}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) Name() string { return s.name }

func (s *Service) Start(ctx context.Context) error {
	if s.immediate {
		if err := s.run(ctx); err != nil && s.stopOnError {
			return err
		}
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.run(ctx); err != nil && s.stopOnError {
				return err
			}
		}
	}
}

func (s *Service) Stop(ctx context.Context) error { return nil }

func (s *Service) run(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("cron: panic in job %s: %v\n%s", s.name, r, debug.Stack())
		}
		if err != nil && s.onError != nil {
			s.onError(err)
		}
	}()
	return s.job(ctx)
}

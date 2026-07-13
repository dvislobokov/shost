package shost

import (
	"errors"
	"fmt"
	"time"
)

// DefaultShutdownTimeout bounds graceful shutdown when
// Builder.WithShutdownTimeout was not called.
const DefaultShutdownTimeout = 30 * time.Second

// Defaults applied to zero-valued RestartPolicy fields.
const (
	DefaultRestartInitialDelay = time.Second
	DefaultRestartMaxDelay     = time.Minute
	DefaultRestartFactor       = 2.0
	DefaultRestartResetAfter   = time.Minute
)

// RestartPolicy controls supervised restarts of a service that exits
// prematurely. Zero-valued fields take the Default* constants above.
type RestartPolicy struct {
	// MaxAttempts is the number of consecutive restarts before giving up
	// and stopping the host. 0 means unlimited.
	MaxAttempts int
	// InitialDelay is the pause before the first restart.
	InitialDelay time.Duration
	// MaxDelay caps the exponential backoff.
	MaxDelay time.Duration
	// Factor multiplies the delay after each consecutive restart.
	Factor float64
	// ResetAfter resets the attempt counter and backoff when the service
	// has been running at least this long before exiting.
	ResetAfter time.Duration
}

func (p RestartPolicy) normalized() RestartPolicy {
	if p.InitialDelay == 0 {
		p.InitialDelay = DefaultRestartInitialDelay
	}
	if p.MaxDelay == 0 {
		p.MaxDelay = DefaultRestartMaxDelay
	}
	if p.Factor == 0 {
		p.Factor = DefaultRestartFactor
	}
	if p.ResetAfter == 0 {
		p.ResetAfter = DefaultRestartResetAfter
	}
	return p
}

func (p RestartPolicy) validate() error {
	if p.MaxAttempts < 0 || p.InitialDelay < 0 || p.MaxDelay < 0 || p.Factor < 0 || p.ResetAfter < 0 {
		return errors.New("shost: restart policy fields must be non-negative")
	}
	n := p.normalized()
	if n.Factor < 1 {
		return fmt.Errorf("shost: restart factor must be >= 1, got %v", n.Factor)
	}
	if n.MaxDelay < n.InitialDelay {
		return fmt.Errorf("shost: restart max delay %v is less than initial delay %v", n.MaxDelay, n.InitialDelay)
	}
	return nil
}

// ServiceOption customizes a single service registration.
type ServiceOption func(*registration)

// WithRestart supervises the service: instead of stopping the host on a
// premature exit, the host restarts it with exponential backoff per the
// given policy. The host stops only when MaxAttempts is exhausted.
func WithRestart(p RestartPolicy) ServiceOption {
	return func(r *registration) {
		np := p.normalized()
		r.restart = &np
		r.restartErr = p.validate()
	}
}

type registration struct {
	svc        Service
	restart    *RestartPolicy
	restartErr error
}

// Builder assembles a Host. All methods return the receiver for chaining;
// configuration errors are accumulated and reported by Build.
type Builder struct {
	services        []registration
	logger          Logger
	environment     Environment
	shutdownTimeout time.Duration
	startTimeout    time.Duration
	onStarted       []func()
	onStopping      []func()
	onStopped       []func()
	observers       []Observer
	errs            []error
}

// New creates an empty Builder.
func New() *Builder {
	return &Builder{shutdownTimeout: DefaultShutdownTimeout}
}

// WithLogger sets the logger used for host lifecycle events.
// Without it the host stays silent; errors are still returned from Run.
func (b *Builder) WithLogger(l Logger) *Builder {
	if l == nil {
		b.errs = append(b.errs, errors.New("shost: WithLogger called with nil logger"))
		return b
	}
	b.logger = l
	return b
}

// WithEnvironment sets the host environment (see EnvironmentFromEnv).
// Defaults to Production.
func (b *Builder) WithEnvironment(e Environment) *Builder {
	if e == "" {
		b.errs = append(b.errs, errors.New("shost: WithEnvironment called with empty environment"))
		return b
	}
	b.environment = e
	return b
}

// WithShutdownTimeout bounds the total time allowed for graceful shutdown
// of all services.
func (b *Builder) WithShutdownTimeout(d time.Duration) *Builder {
	if d <= 0 {
		b.errs = append(b.errs, fmt.Errorf("shost: shutdown timeout must be positive, got %v", d))
		return b
	}
	b.shutdownTimeout = d
	return b
}

// WithStartTimeout bounds the total time spent waiting for readiness of
// services implementing Readier during startup. Without it the host waits
// indefinitely.
func (b *Builder) WithStartTimeout(d time.Duration) *Builder {
	if d <= 0 {
		b.errs = append(b.errs, fmt.Errorf("shost: start timeout must be positive, got %v", d))
		return b
	}
	b.startTimeout = d
	return b
}

// AddService registers a service. Services start in registration order and
// stop in reverse order. Names must be unique and non-empty.
func (b *Builder) AddService(s Service, opts ...ServiceOption) *Builder {
	if s == nil {
		b.errs = append(b.errs, errors.New("shost: AddService called with nil service"))
		return b
	}
	name := s.Name()
	if name == "" {
		b.errs = append(b.errs, errors.New("shost: service has empty name"))
		return b
	}
	for _, existing := range b.services {
		if existing.svc.Name() == name {
			b.errs = append(b.errs, fmt.Errorf("shost: duplicate service name %q", name))
			return b
		}
	}
	reg := registration{svc: s}
	for _, opt := range opts {
		opt(&reg)
	}
	if reg.restartErr != nil {
		b.errs = append(b.errs, fmt.Errorf("service %q: %w", name, reg.restartErr))
		return b
	}
	b.services = append(b.services, reg)
	return b
}

// OnStarted registers a hook invoked once all services have been launched
// (and reported ready, for services implementing Readier). Hooks run in
// registration order; panics are recovered and logged.
func (b *Builder) OnStarted(fn func()) *Builder {
	return b.addHook(&b.onStarted, "OnStarted", fn)
}

// OnStopping registers a hook invoked when shutdown begins, before any
// service is stopped.
func (b *Builder) OnStopping(fn func()) *Builder {
	return b.addHook(&b.onStopping, "OnStopping", fn)
}

// OnStopped registers a hook invoked after all services have stopped.
func (b *Builder) OnStopped(fn func()) *Builder {
	return b.addHook(&b.onStopped, "OnStopped", fn)
}

// WithObserver registers a lifecycle observer (see Observer). Multiple
// observers are invoked in registration order.
func (b *Builder) WithObserver(o Observer) *Builder {
	b.observers = append(b.observers, o)
	return b
}

func (b *Builder) addHook(hooks *[]func(), name string, fn func()) *Builder {
	if fn == nil {
		b.errs = append(b.errs, fmt.Errorf("shost: %s called with nil hook", name))
		return b
	}
	*hooks = append(*hooks, fn)
	return b
}

// Build validates the configuration and returns the Host.
func (b *Builder) Build() (*Host, error) {
	if len(b.errs) > 0 {
		return nil, errors.Join(b.errs...)
	}
	log := b.logger
	if log == nil {
		log = nopLogger{}
	}
	env := b.environment
	if env == "" {
		env = Production
	}
	return &Host{
		services:        append([]registration(nil), b.services...),
		log:             log,
		environment:     env,
		shutdownTimeout: b.shutdownTimeout,
		startTimeout:    b.startTimeout,
		onStarted:       b.onStarted,
		onStopping:      b.onStopping,
		onStopped:       b.onStopped,
		observers:       b.observers,
		shutdownCh:      make(chan struct{}),
		stoppingCh:      make(chan struct{}),
	}, nil
}

// MustBuild is Build panicking on configuration errors. Intended for main().
func (b *Builder) MustBuild() *Host {
	h, err := b.Build()
	if err != nil {
		panic(err)
	}
	return h
}

package shost

import (
	"errors"
	"fmt"
	"time"
)

// DefaultShutdownTimeout bounds graceful shutdown when
// Builder.WithShutdownTimeout was not called.
const DefaultShutdownTimeout = 30 * time.Second

// Builder assembles a Host. All methods return the receiver for chaining;
// configuration errors are accumulated and reported by Build.
type Builder struct {
	services        []Service
	logger          Logger
	shutdownTimeout time.Duration
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

// AddService registers a service. Services start in registration order and
// stop in reverse order. Names must be unique and non-empty.
func (b *Builder) AddService(s Service) *Builder {
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
		if existing.Name() == name {
			b.errs = append(b.errs, fmt.Errorf("shost: duplicate service name %q", name))
			return b
		}
	}
	b.services = append(b.services, s)
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
	return &Host{
		services:        append([]Service(nil), b.services...),
		log:             log,
		shutdownTimeout: b.shutdownTimeout,
		shutdownCh:      make(chan struct{}),
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

package shost

import "context"

// Service is a long-running component managed by the Host.
//
// Start must block for the lifetime of the service and return only after
// ctx is canceled (graceful exit) or the service fails. A Service that
// returns from Start before shutdown was requested — with or without an
// error — stops the whole host.
//
// Stop is called during shutdown, after the Start context has been
// canceled, in reverse registration order. The passed ctx carries the
// shutdown deadline; Stop must respect it.
type Service interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Readier is an optional interface a Service may implement to signal
// readiness. When implemented, the Host waits for the returned channel to
// be closed before launching the next registered service, bounded by
// Builder.WithStartTimeout. The channel must be closed by Start once the
// service is operational (e.g. a listener is accepting connections).
type Readier interface {
	Ready() <-chan struct{}
}

// ServiceFunc adapts a blocking run function into a Service with no
// dedicated Stop logic: cancellation of the Start context is the only
// stop signal.
func ServiceFunc(name string, run func(ctx context.Context) error) Service {
	return &funcService{name: name, run: run}
}

type funcService struct {
	name string
	run  func(ctx context.Context) error
}

func (s *funcService) Name() string                    { return s.name }
func (s *funcService) Start(ctx context.Context) error { return s.run(ctx) }
func (s *funcService) Stop(ctx context.Context) error  { return nil }

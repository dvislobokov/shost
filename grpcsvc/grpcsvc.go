// Package grpcsvc adapts *grpc.Server to the shost.Service contract:
// readiness (shost.Readier) once the listener is accepting, graceful
// draining via GracefulStop under the host's deadline, and a forceful
// Stop when the deadline expires.
//
// Separate go module: unlike the shost core, it depends on
// google.golang.org/grpc.
package grpcsvc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc"
)

// Service runs a *grpc.Server as a shost.Service.
type Service struct {
	name      string
	addr      string
	srv       *grpc.Server
	ready     chan struct{}
	readyOnce sync.Once
	listAddr  atomic.Value // string, set once listening
}

// Option customizes a Service.
type Option func(*Service)

// WithName overrides the default service name ("grpc <addr>").
func WithName(name string) Option {
	return func(s *Service) { s.name = name }
}

// New creates a gRPC server service listening on addr. Register your
// services on srv before passing it in.
func New(addr string, srv *grpc.Server, opts ...Option) *Service {
	if srv == nil {
		panic("grpcsvc: New called with nil server")
	}
	s := &Service{
		name:  "grpc " + addr,
		addr:  addr,
		srv:   srv,
		ready: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) Name() string { return s.name }

// Ready implements shost.Readier: the channel closes once the listener is
// accepting connections.
func (s *Service) Ready() <-chan struct{} { return s.ready }

// Addr returns the actual listen address (useful with ":0"), or "" before
// the service is ready.
func (s *Service) Addr() string {
	if v := s.listAddr.Load(); v != nil {
		return v.(string)
	}
	return ""
}

// Start listens and serves until the host initiates shutdown. Graceful
// termination is driven by Stop; Start unblocks when Serve returns.
func (s *Service) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("grpcsvc: listen %s: %w", s.addr, err)
	}
	s.listAddr.Store(ln.Addr().String())
	s.readyOnce.Do(func() { close(s.ready) })

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		if errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return err
	case <-ctx.Done():
		// Draining is driven by Stop; wait for Serve to return.
		err := <-serveErr
		if err == nil || errors.Is(err, grpc.ErrServerStopped) {
			return ctx.Err()
		}
		return err
	}
}

// Stop drains the server gracefully under ctx's deadline; when the
// deadline expires, remaining connections and streams are closed
// forcefully.
func (s *Service) Stop(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.srv.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.srv.Stop()
		<-done
		return fmt.Errorf("grpcsvc: graceful shutdown timed out, connections closed forcefully: %w", ctx.Err())
	}
}

// Package httpsvc adapts net/http servers to the shost.Service contract:
// graceful shutdown via http.Server.Shutdown under the host's deadline,
// and readiness (shost.Readier) signaled once the listener is accepting.
package httpsvc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
)

// Service runs an *http.Server as a shost.Service.
type Service struct {
	name      string
	srv       *http.Server
	ready     chan struct{}
	readyOnce sync.Once
	addr      atomic.Value // string, set once listening
}

// Option customizes a Service.
type Option func(*Service)

// WithName overrides the default service name ("http <addr>").
func WithName(name string) Option {
	return func(s *Service) { s.name = name }
}

// WithServer customizes the underlying *http.Server (timeouts, TLS config,
// error log, etc.) before it starts.
func WithServer(configure func(*http.Server)) Option {
	return func(s *Service) { configure(s.srv) }
}

// New creates an HTTP server service listening on addr.
func New(addr string, handler http.Handler, opts ...Option) *Service {
	s := &Service{
		name:  "http " + addr,
		srv:   &http.Server{Addr: addr, Handler: handler},
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
	if v := s.addr.Load(); v != nil {
		return v.(string)
	}
	return ""
}

// Start listens and serves until the host initiates shutdown. Graceful
// termination is driven by Stop; Start unblocks when Serve returns.
func (s *Service) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return fmt.Errorf("httpsvc: listen %s: %w", s.srv.Addr, err)
	}
	s.addr.Store(ln.Addr().String())
	s.readyOnce.Do(func() { close(s.ready) })

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		// Shutdown is driven by Stop; wait for Serve to drain.
		err := <-serveErr
		if errors.Is(err, http.ErrServerClosed) {
			return ctx.Err()
		}
		return err
	}
}

// Stop gracefully shuts the server down under ctx's deadline; when the
// deadline expires, remaining connections are closed forcefully.
func (s *Service) Stop(ctx context.Context) error {
	err := s.srv.Shutdown(ctx)
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		s.srv.Close()
		return fmt.Errorf("httpsvc: graceful shutdown timed out, connections closed forcefully: %w", err)
	}
	return err
}

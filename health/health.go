// Package health provides health checks for shost applications: a Checker
// registry with liveness and readiness HTTP handlers suitable for
// Kubernetes probes.
//
// Readiness is wired to the host lifecycle explicitly:
//
//	reg := health.NewRegistry()
//	host := shost.New().
//		OnStarted(func() { reg.SetReady(true) }).
//		OnStopping(func() { reg.SetReady(false) }).
//		AddService(httpsvc.New(":8080", mux)).
//		MustBuild()
//	reg.Mount(mux)
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
)

// Checker is a single health check. Check returns nil when healthy.
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

// CheckerFunc adapts a function to the Checker interface.
func CheckerFunc(name string, fn func(ctx context.Context) error) Checker {
	return &funcChecker{name: name, fn: fn}
}

type funcChecker struct {
	name string
	fn   func(ctx context.Context) error
}

func (c *funcChecker) Name() string                    { return c.name }
func (c *funcChecker) Check(ctx context.Context) error { return c.fn(ctx) }

// Registry holds checkers and the readiness flag.
type Registry struct {
	mu       sync.RWMutex
	checkers []Checker
	ready    atomic.Bool
}

// NewRegistry creates a Registry with the given checkers. Readiness starts
// as false until SetReady(true) — typically from the host's OnStarted hook.
func NewRegistry(checkers ...Checker) *Registry {
	r := &Registry{}
	for _, c := range checkers {
		r.Add(c)
	}
	return r
}

// Add registers a checker. Safe for concurrent use.
func (r *Registry) Add(c Checker) {
	if c == nil {
		panic("health: Add called with nil checker")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkers = append(r.checkers, c)
}

// SetReady flips the readiness flag used by ReadyHandler.
func (r *Registry) SetReady(v bool) { r.ready.Store(v) }

// Ready reports the current readiness flag.
func (r *Registry) Ready() bool { return r.ready.Load() }

// LiveHandler serves liveness: 200 when every checker passes, 503 otherwise.
// The response body is JSON: {"status":"ok|unhealthy","checks":{...}}.
func (r *Registry) LiveHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		healthy, checks := r.runChecks(req.Context())
		writeStatus(w, healthy, checks)
	})
}

// ReadyHandler serves readiness: 200 only when the readiness flag is set
// AND every checker passes.
func (r *Registry) ReadyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !r.ready.Load() {
			writeStatus(w, false, map[string]string{"host": "not ready"})
			return
		}
		healthy, checks := r.runChecks(req.Context())
		writeStatus(w, healthy, checks)
	})
}

// MountOption customizes the paths used by Mount.
type MountOption func(*mountOptions)

type mountOptions struct {
	live  string
	ready string
}

// WithLivePath overrides the liveness path (default "/healthz").
func WithLivePath(path string) MountOption {
	return func(o *mountOptions) { o.live = path }
}

// WithReadyPath overrides the readiness path (default "/readyz").
func WithReadyPath(path string) MountOption {
	return func(o *mountOptions) { o.ready = path }
}

// Mount registers LiveHandler at /healthz and ReadyHandler at /readyz.
// Override either path with WithLivePath / WithReadyPath:
//
//	reg.Mount(mux, health.WithLivePath("/live"), health.WithReadyPath("/ready"))
func (r *Registry) Mount(mux *http.ServeMux, opts ...MountOption) {
	o := mountOptions{live: "/healthz", ready: "/readyz"}
	for _, opt := range opts {
		opt(&o)
	}
	mux.Handle(o.live, r.LiveHandler())
	mux.Handle(o.ready, r.ReadyHandler())
}

func (r *Registry) runChecks(ctx context.Context) (bool, map[string]string) {
	r.mu.RLock()
	checkers := append([]Checker(nil), r.checkers...)
	r.mu.RUnlock()

	healthy := true
	checks := make(map[string]string, len(checkers))
	for _, c := range checkers {
		if err := safeCheck(c, ctx); err != nil {
			healthy = false
			checks[c.Name()] = err.Error()
		} else {
			checks[c.Name()] = "ok"
		}
	}
	return healthy, checks
}

func safeCheck(c Checker, ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return c.Check(ctx)
}

type response struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

func writeStatus(w http.ResponseWriter, healthy bool, checks map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	status := "ok"
	if !healthy {
		status = "unhealthy"
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(response{Status: status, Checks: checks})
}

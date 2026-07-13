package shost

import (
	"fmt"
	"time"
)

// Observer receives host lifecycle events, in the style of
// net/http/httptrace.ClientTrace: any field may be nil. Callbacks are
// invoked synchronously from the host loop — keep them fast. Panics in
// callbacks are recovered and logged. Register with Builder.WithObserver;
// the shost/otel module provides an OpenTelemetry implementation.
type Observer struct {
	// HostStarted fires once all services are launched and ready.
	HostStarted func()
	// HostStopped fires last, with the final Run error (nil when clean).
	HostStopped func(err error)
	// ServiceStarted fires when a service's Start goroutine is launched.
	ServiceStarted func(name string)
	// ServiceReady fires when a Readier service signals readiness.
	ServiceReady func(name string)
	// ServiceRestarting fires before a supervised restart wait begins.
	ServiceRestarting func(name string, attempt int, delay time.Duration, err error)
	// ServiceStopped fires when a service finished stopping during
	// shutdown; err is non-nil on Stop errors or shutdown timeout.
	ServiceStopped func(name string, elapsed time.Duration, err error)
	// ServiceFailed fires when a service exit is about to stop the host.
	ServiceFailed func(name string, err error)
}

// observe runs fn for every registered observer, recovering panics.
// fn is responsible for the nil check of the field it invokes.
func (h *Host) observe(fn func(Observer)) {
	for _, o := range h.observers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					h.log.Error(fmt.Errorf("panic: %v", r), "panic in observer callback")
				}
			}()
			fn(o)
		}()
	}
}

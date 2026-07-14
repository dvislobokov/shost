package shosttest

import (
	"sync"
	"time"

	"github.com/dvislobokov/shost"
)

// Event kinds recorded by Recorder, mirroring the shost.Observer fields.
const (
	HostStarted       = "HostStarted"
	HostStopped       = "HostStopped"
	ServiceStarted    = "ServiceStarted"
	ServiceReady      = "ServiceReady"
	ServiceRestarting = "ServiceRestarting"
	ServiceStopped    = "ServiceStopped"
	ServiceFailed     = "ServiceFailed"
)

// Event is a single recorded lifecycle event. Fields beyond Kind are
// populated only where the corresponding Observer callback provides them.
type Event struct {
	Kind    string
	Service string
	Err     error
	Attempt int
	Delay   time.Duration
	Elapsed time.Duration
}

// Recorder records shost lifecycle events for assertions. Register it via
// Builder.WithObserver(rec.Observer()). Safe for concurrent use.
type Recorder struct {
	mu     sync.Mutex
	events []Event
}

// NewRecorder creates an empty Recorder.
func NewRecorder() *Recorder { return &Recorder{} }

// Observer returns the shost.Observer feeding this Recorder.
func (r *Recorder) Observer() shost.Observer {
	return shost.Observer{
		HostStarted: func() { r.add(Event{Kind: HostStarted}) },
		HostStopped: func(err error) { r.add(Event{Kind: HostStopped, Err: err}) },
		ServiceStarted: func(name string) {
			r.add(Event{Kind: ServiceStarted, Service: name})
		},
		ServiceReady: func(name string) {
			r.add(Event{Kind: ServiceReady, Service: name})
		},
		ServiceRestarting: func(name string, attempt int, delay time.Duration, err error) {
			r.add(Event{Kind: ServiceRestarting, Service: name, Attempt: attempt, Delay: delay, Err: err})
		},
		ServiceStopped: func(name string, elapsed time.Duration, err error) {
			r.add(Event{Kind: ServiceStopped, Service: name, Elapsed: elapsed, Err: err})
		},
		ServiceFailed: func(name string, err error) {
			r.add(Event{Kind: ServiceFailed, Service: name, Err: err})
		},
	}
}

func (r *Recorder) add(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

// Events returns a copy of all recorded events in order.
func (r *Recorder) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

// Has reports whether an event of the given kind was recorded for the
// given service; pass "" to match host-level events or any service.
func (r *Recorder) Has(kind, service string) bool {
	for _, e := range r.Events() {
		if e.Kind == kind && (service == "" || e.Service == service) {
			return true
		}
	}
	return false
}

// WaitFor polls until an event matching Has(kind, service) is recorded or
// the timeout elapses, reporting whether it was found. Useful for events
// produced asynchronously, like ServiceRestarting.
func (r *Recorder) WaitFor(kind, service string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if r.Has(kind, service) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

//go:build windows

package winsvc

import (
	"context"
	"os"
	"time"

	"github.com/dvislobokov/shost"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
)

// IsWindowsService reports whether the process was started by the Service
// Control Manager.
func IsWindowsService() bool {
	v, err := svc.IsWindowsService()
	return err == nil && v
}

// Run builds the host from b and runs it under SCM control when the
// process is a Windows service, or as a plain console application
// otherwise. Under SCM, host startup/shutdown errors are also written to
// the Event Log (source = service name; created by Install).
func Run(b *shost.Builder, opts ...Option) error {
	o := buildOptions(os.Args[0], opts)
	if !IsWindowsService() {
		host, err := b.Build()
		if err != nil {
			return err
		}
		return host.Run()
	}

	started := make(chan struct{})
	host, err := b.OnStarted(func() { close(started) }).Build()
	if err != nil {
		logEvent(o.name, err)
		return err
	}

	h := &handler{host: host, started: started}
	if err := svc.Run(o.name, h); err != nil {
		logEvent(o.name, err)
		return err
	}
	if h.runErr != nil {
		logEvent(o.name, h.runErr)
	}
	return h.runErr
}

// logEvent best-effort writes err to the Event Log under the given source.
func logEvent(source string, err error) {
	el, openErr := eventlog.Open(source)
	if openErr != nil {
		return
	}
	defer el.Close()
	_ = el.Error(1, err.Error())
}

const (
	startWaitHint = 30 * time.Second
	// stopSlack pads the host's shutdown timeout in the SCM wait hint so
	// SCM does not kill the process while the host is still draining.
	stopSlack = 10 * time.Second
)

// handler adapts the host lifecycle to svc.Handler.
type handler struct {
	host    *shost.Host
	started <-chan struct{}
	runErr  error
}

func (h *handler) Execute(_ []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (svcSpecific bool, exitCode uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptParamChange

	s <- svc.Status{State: svc.StartPending, WaitHint: millis(startWaitHint)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- h.host.RunContext(ctx) }()

	select {
	case <-h.started:
	case err := <-runDone:
		// Startup task failure, service failure or readiness timeout.
		h.runErr = err
		return h.exit(s, err)
	}
	s <- svc.Status{State: svc.Running, Accepts: accepts}

	for {
		select {
		case err := <-runDone:
			// A service failed (or exhausted its restart policy): the
			// host stopped on its own.
			h.runErr = err
			return h.exit(s, err)
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				s <- c.CurrentStatus
			case svc.ParamChange:
				h.host.Reload()
			case svc.Stop, svc.Shutdown:
				h.host.Shutdown()
				h.runErr = h.waitStop(runDone, s)
				return h.exit(s, h.runErr)
			}
		}
	}
}

// waitStop reports STOP_PENDING with advancing checkpoints while the host
// drains, so SCM keeps waiting instead of killing the process.
func (h *handler) waitStop(runDone <-chan error, s chan<- svc.Status) error {
	waitHint := millis(h.host.ShutdownTimeout() + stopSlack)
	checkpoint := uint32(1)
	s <- svc.Status{State: svc.StopPending, CheckPoint: checkpoint, WaitHint: waitHint}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-runDone:
			return err
		case <-ticker.C:
			checkpoint++
			s <- svc.Status{State: svc.StopPending, CheckPoint: checkpoint, WaitHint: waitHint}
		}
	}
}

func (h *handler) exit(s chan<- svc.Status, err error) (bool, uint32) {
	s <- svc.Status{State: svc.Stopped}
	if err != nil {
		return true, 1
	}
	return false, 0
}

func millis(d time.Duration) uint32 {
	return uint32(d / time.Millisecond)
}

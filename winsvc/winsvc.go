// Package winsvc runs a shost application as a Windows service — the
// analog of Microsoft.Extensions.Hosting.WindowsServices. A Go binary
// started by the Service Control Manager must speak the SCM protocol
// within ~30 seconds or the start fails (error 1053) — and SCM sends
// control codes, not signals, so the host's SIGTERM handling never fires.
// winsvc bridges the two worlds:
//
//	func main() {
//		b := shost.New().
//			AddService(...).
//			OnReload(reloadConfig)
//		if err := winsvc.Run(b, winsvc.WithName("my-agent")); err != nil {
//			os.Exit(1)
//		}
//	}
//
// Under SCM: START_PENDING while startup tasks and services come up,
// RUNNING after the host reports started, Stop/Shutdown controls trigger
// graceful shutdown with STOP_PENDING checkpoints while services drain,
// and PARAMCHANGE (sc control <name> paramchange) invokes Host.Reload.
// Startup and shutdown errors are written to the Windows Event Log.
//
// Outside SCM (a terminal, a container, any other OS) Run falls back to
// Host.Run, so the same binary works everywhere.
//
// Separate go module: depends on golang.org/x/sys. On non-Windows
// platforms the package compiles to the plain fallback.
package winsvc

import "path/filepath"

// options holds cross-platform Run configuration.
type options struct {
	name string
}

// Option customizes Run.
type Option func(*options)

// WithName sets the service name used for SCM registration and the Event
// Log source. Defaults to the executable name. Must match the name the
// service was installed under.
func WithName(name string) Option {
	return func(o *options) { o.name = name }
}

func buildOptions(exe string, opts []Option) options {
	o := options{name: strippedExeName(exe)}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

func strippedExeName(exe string) string {
	base := filepath.Base(exe)
	if ext := filepath.Ext(base); ext != "" {
		base = base[:len(base)-len(ext)]
	}
	return base
}

//go:build unix

package shost

import (
	"os"
	"os/signal"
	"syscall"
)

// notifyReloadSignal maps SIGHUP to Reload for the lifetime of Run.
func (h *Host) notifyReloadSignal() (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				h.Reload()
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

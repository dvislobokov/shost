//go:build !unix

package shost

// notifyReloadSignal is a no-op where SIGHUP does not exist; service
// managers trigger Reload through adapters instead (see winsvc).
func (h *Host) notifyReloadSignal() (stop func()) {
	return func() {}
}

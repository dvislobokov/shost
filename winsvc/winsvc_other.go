//go:build !windows

package winsvc

import "github.com/dvislobokov/shost"

// IsWindowsService reports false on non-Windows platforms.
func IsWindowsService() bool { return false }

// Run builds the host and runs it as a plain application: on non-Windows
// platforms there is no SCM, and the host's own signal handling (SIGTERM,
// SIGHUP → Reload) applies.
func Run(b *shost.Builder, opts ...Option) error {
	_ = buildOptions("", opts)
	host, err := b.Build()
	if err != nil {
		return err
	}
	return host.Run()
}

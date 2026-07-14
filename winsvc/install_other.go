//go:build !windows

package winsvc

import "errors"

// InstallConfig describes the service registration created by Install.
// See the windows build for field documentation.
type InstallConfig struct {
	DisplayName      string
	Description      string
	Args             []string
	Manual           bool
	DelayedAutoStart bool
}

var errNotWindows = errors.New("winsvc: service installation is only available on Windows")

// Install is unavailable on non-Windows platforms.
func Install(name, exePath string, cfg InstallConfig) error { return errNotWindows }

// Uninstall is unavailable on non-Windows platforms.
func Uninstall(name string) error { return errNotWindows }

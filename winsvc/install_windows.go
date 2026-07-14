//go:build windows

package winsvc

import (
	"fmt"

	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

// InstallConfig describes the service registration created by Install.
type InstallConfig struct {
	// DisplayName is shown in services.msc; defaults to the service name.
	DisplayName string
	// Description is shown in services.msc.
	Description string
	// Args are appended to the service command line on every start.
	Args []string
	// Manual registers the service for manual start instead of automatic.
	Manual bool
	// DelayedAutoStart delays automatic start until after boot-critical
	// services (ignored when Manual is set).
	DelayedAutoStart bool
}

// Install registers the executable at exePath as a Windows service and
// creates the matching Event Log source. Typically wired to an
// "install" CLI flag and run elevated:
//
//	winsvc.Install("my-agent", exePath, winsvc.InstallConfig{
//		DisplayName: "My Agent",
//		Description: "Collects system metrics.",
//	})
func Install(name, exePath string, cfg InstallConfig) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("winsvc: connect to SCM (requires elevation): %w", err)
	}
	defer m.Disconnect()

	if s, err := m.OpenService(name); err == nil {
		s.Close()
		return fmt.Errorf("winsvc: service %s already exists", name)
	}

	c := mgr.Config{
		DisplayName:      cfg.DisplayName,
		Description:      cfg.Description,
		StartType:        mgr.StartAutomatic,
		DelayedAutoStart: cfg.DelayedAutoStart,
	}
	if c.DisplayName == "" {
		c.DisplayName = name
	}
	if cfg.Manual {
		c.StartType = mgr.StartManual
		c.DelayedAutoStart = false
	}

	s, err := m.CreateService(name, exePath, c, cfg.Args...)
	if err != nil {
		return fmt.Errorf("winsvc: create service %s: %w", name, err)
	}
	defer s.Close()

	if err := eventlog.InstallAsEventCreate(name, eventlog.Error|eventlog.Warning|eventlog.Info); err != nil {
		// The service is functional without its own source; don't fail
		// the install over it (a previous install may have left it).
		_ = err
	}
	return nil
}

// Uninstall removes the service registration and its Event Log source.
// The service should be stopped first.
func Uninstall(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("winsvc: connect to SCM (requires elevation): %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("winsvc: service %s is not installed: %w", name, err)
	}
	defer s.Close()

	if err := s.Delete(); err != nil {
		return fmt.Errorf("winsvc: delete service %s: %w", name, err)
	}
	_ = eventlog.Remove(name)
	return nil
}

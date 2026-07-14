// Package single guarantees a single running instance of an application
// per machine — a common requirement for system agents, where a second
// copy means duplicated metrics and port conflicts.
//
// The lock is tied to the process: it is released by the operating system
// even when the process crashes (flock on Unix-like systems, an exclusive
// file handle on Windows), so no stale-pidfile handling is needed.
//
//	lock, err := single.Acquire(single.DefaultPath("my-agent"))
//	if errors.Is(err, single.ErrAlreadyRunning) {
//		fmt.Fprintln(os.Stderr, "my-agent is already running")
//		os.Exit(1)
//	}
//	defer lock.Release()
//
// Acquire the lock in main, before building the host, and hold it for the
// process lifetime. Standard library only.
package single

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrAlreadyRunning is returned by Acquire when another process holds the
// lock.
var ErrAlreadyRunning = errors.New("single: another instance is already running")

// Lock is a held machine-wide lock.
type Lock struct {
	f    *os.File
	path string
}

// Acquire takes an exclusive machine-wide lock identified by the file at
// path. It returns ErrAlreadyRunning (wrapped) when another process holds
// it, and never blocks.
func Acquire(path string) (*Lock, error) {
	f, err := open(path)
	if err != nil {
		if errors.Is(err, ErrAlreadyRunning) {
			return nil, fmt.Errorf("single: lock %s: %w", path, ErrAlreadyRunning)
		}
		return nil, fmt.Errorf("single: lock %s: %w", path, err)
	}
	// Best effort: the pid helps a human inspecting the lock file. The
	// lock itself is the file handle, not the content.
	_ = f.Truncate(0)
	fmt.Fprintf(f, "%d\n", os.Getpid())
	return &Lock{f: f, path: path}, nil
}

// Release releases the lock. The lock is also released automatically when
// the process exits, however it happened.
func (l *Lock) Release() error {
	err := release(l.f, l.path)
	l.f = nil
	return err
}

// Path returns the lock file path.
func (l *Lock) Path() string { return l.path }

// DefaultPath returns a conventional lock file location for the given
// application name: the system temporary directory. Agents running as
// root/SYSTEM may prefer an explicit path like /run/<name>.lock.
func DefaultPath(name string) string {
	return filepath.Join(os.TempDir(), name+".lock")
}

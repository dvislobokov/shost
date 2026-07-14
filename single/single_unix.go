//go:build unix

package single

import (
	"errors"
	"os"
	"syscall"
)

// open takes a non-blocking flock on the file. The lock lives on the open
// file description, so the kernel releases it when the process dies.
func open(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	return f, nil
}

// release closes the descriptor, dropping the flock. The file itself is
// left in place: removing it would open a race where a third process
// locks a new inode while a second still holds the old one.
func release(f *os.File, _ string) error {
	if f == nil {
		return nil
	}
	return f.Close()
}

//go:build windows

package single

import (
	"errors"
	"os"
	"syscall"
)

// open creates the file with an empty share mode: any second CreateFile —
// from this or another process — fails with ERROR_SHARING_VIOLATION until
// the handle is closed. Windows closes handles of dead processes, so a
// crash releases the lock.
func open(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		0, // no sharing — this is the lock
		nil,
		syscall.OPEN_ALWAYS,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0)
	if err != nil {
		if errors.Is(err, syscall.Errno(32)) { // ERROR_SHARING_VIOLATION
			return nil, ErrAlreadyRunning
		}
		return nil, &os.PathError{Op: "CreateFile", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// release closes the handle and removes the file (best effort — a fresh
// instance recreates it anyway).
func release(f *os.File, path string) error {
	if f == nil {
		return nil
	}
	err := f.Close()
	_ = os.Remove(path)
	return err
}

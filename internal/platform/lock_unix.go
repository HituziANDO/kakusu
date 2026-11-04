//go:build !windows

package platform

import (
	"errors"
	"os"
	"syscall"
)

// TryLock takes an exclusive lock on f without waiting. It returns false, with
// no error, if another process holds the lock.
func TryLock(f *os.File) (bool, error) {
	err := flock(f, syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

// Lock waits until it takes an exclusive lock on f.
func Lock(f *os.File) error {
	return flock(f, syscall.LOCK_EX)
}

// LockUnsupported reports whether err from TryLock means that the filesystem
// cannot lock files. errors.ErrUnsupported matches ENOSYS, ENOTSUP, and
// EOPNOTSUPP.
func LockUnsupported(err error) bool {
	return errors.Is(err, errors.ErrUnsupported) || errors.Is(err, syscall.ENOLCK)
}

// flock calls flock(2) again when a signal interrupts it. The lock belongs to
// the open file and is released when the process exits.
func flock(f *os.File, how int) error {
	for {
		err := syscall.Flock(int(f.Fd()), how)
		if err != syscall.EINTR {
			return err
		}
	}
}

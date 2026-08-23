//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package instill

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

type platformRootLockProvider struct{}

func (platformRootLockProvider) open(path string) (rootLockHandle, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // The canonical root selects the persistent lock path.
	if err != nil {
		return nil, err
	}
	return &unixRootLockHandle{file: file}, nil
}

type unixRootLockHandle struct {
	file *os.File
}

func (h *unixRootLockHandle) tryLock() (bool, error) {
	err := unix.Flock(int(h.file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

func (h *unixRootLockHandle) unlock() error {
	return unix.Flock(int(h.file.Fd()), unix.LOCK_UN)
}

func (h *unixRootLockHandle) close() error {
	return h.file.Close()
}

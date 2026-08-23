//go:build windows

package instill

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

type platformRootLockProvider struct{}

func (platformRootLockProvider) open(path string) (rootLockHandle, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // The canonical root selects the persistent lock path.
	if err != nil {
		return nil, err
	}
	return &windowsRootLockHandle{file: file}, nil
}

type windowsRootLockHandle struct {
	file *os.File
}

func (h *windowsRootLockHandle) tryLock() (bool, error) {
	err := windows.LockFileEx(
		windows.Handle(h.file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&windows.Overlapped{},
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return false, nil
	}
	return err == nil, err
}

func (h *windowsRootLockHandle) unlock() error {
	return windows.UnlockFileEx(windows.Handle(h.file.Fd()), 0, 1, 0, &windows.Overlapped{})
}

func (h *windowsRootLockHandle) close() error {
	return h.file.Close()
}

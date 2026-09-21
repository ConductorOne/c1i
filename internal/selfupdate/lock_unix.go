//go:build !windows

package selfupdate

import (
	"fmt"
	"os"
	"syscall"
)

// LockExecutable acquires a non-blocking advisory lock for an in-place update.
func LockExecutable(execPath string) (func(), error) {
	file, err := os.OpenFile(execPath+".upgrade-lock", os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- execPath comes from os.Executable
	if err != nil {
		return nil, fmt.Errorf("opening upgrade lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("another c1i upgrade is already running: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

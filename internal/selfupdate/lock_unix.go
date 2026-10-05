//go:build !windows

package selfupdate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LockExecutable takes a non-blocking advisory lock on execPath's directory.
// Locking the directory rather than a sidecar file leaves nothing behind, and
// it is where the replacement is staged and renamed.
func LockExecutable(execPath string) (func(), error) {
	dir := filepath.Dir(execPath)
	f, err := os.Open(dir) // #nosec G304 -- the install directory, from os.Executable
	if err != nil {
		return nil, fmt.Errorf("opening %s to lock it: %w", dir, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("another c1i upgrade is already running in %s", dir)
		}
		return nil, fmt.Errorf("locking %s: %w", dir, err)
	}
	return func() { _ = f.Close() }, nil // closing releases the lock
}

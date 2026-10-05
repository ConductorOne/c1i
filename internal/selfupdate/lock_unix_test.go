//go:build !windows

package selfupdate

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestLockExecutableWhereDirectoryLocksFail(t *testing.T) {
	orig := flock
	t.Cleanup(func() { flock = orig })
	execPath := filepath.Join(t.TempDir(), "c1i")

	// NFS and similar can't flock a read-only directory fd; proceed unlocked.
	for _, errno := range []syscall.Errno{syscall.EBADF, syscall.ENOLCK, syscall.EOPNOTSUPP, syscall.ENOTSUP} {
		flock = func(int, int) error { return errno }
		unlock, err := LockExecutable(execPath)
		if err != nil {
			t.Errorf("%v: LockExecutable = %v, want to proceed unlocked", errno, err)
			continue
		}
		unlock()
	}

	flock = func(int, int) error { return syscall.EWOULDBLOCK }
	if _, err := LockExecutable(execPath); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("held lock: LockExecutable = %v, want an already-running error", err)
	}
	flock = func(int, int) error { return syscall.EIO }
	if _, err := LockExecutable(execPath); err == nil {
		t.Error("EIO: LockExecutable = nil, want an error")
	}
}

package cmd

import (
	"os"
	"testing"
)

// TestIsTerminalFalseForDevNull pins C106: /dev/null is a character device, so
// the old os.ModeCharDevice check wrongly reported it as a TTY -- which sent a
// redirected `auth login` into the interactive prompt instead of the clean
// "url is required" error.
func TestIsTerminalFalseForDevNull(t *testing.T) {
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer func() { _ = devnull.Close() }()

	saved := os.Stdin
	os.Stdin = devnull
	defer func() { os.Stdin = saved }()

	if isTerminal() {
		t.Error("isTerminal() = true for /dev/null, want false")
	}
}

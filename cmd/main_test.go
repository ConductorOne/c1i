package cmd

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain swaps in go-keyring's in-memory mock for the whole package: it is
// process-global with no reset, so setting it per test would make keyring
// state depend on test order.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

package cmd

import (
	"os"
	"testing"

	"github.com/ConductorOne/c1i/internal/testenv"
)

// TestMain isolates HOME/config dirs, C1I_* and the keyring (see testenv.Run)
// so no test can reach the developer's real config or credentials.
func TestMain(m *testing.M) { os.Exit(testenv.Run(m)) }

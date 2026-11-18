package cmd

import (
	"os"
	"testing"

	"github.com/ConductorOne/c1i/internal/testenv"
)

// TestMain isolates HOME/config dirs, C1I_* and the keyring (see testenv.Run)
// so no test can reach the developer's real config or credentials, and fails
// the run if a test leaves a command writer set (checkWritersAfterRun).
func TestMain(m *testing.M) { os.Exit(checkWritersAfterRun(testenv.Run(m))) }

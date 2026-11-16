package client

import (
	"os"
	"testing"

	"github.com/ConductorOne/c1i/internal/testenv"
)

func TestMain(m *testing.M) { os.Exit(testenv.Run(m)) }

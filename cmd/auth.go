package cmd

import (
	"runtime"

	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication credentials",
}

func init() {
	rootCmd.AddCommand(authCmd)
}

// keyringName returns the OS-specific name for the system credential store, so
// user-facing messages match what people actually call it on their platform
// ("Keychain" on macOS, "Credential Manager" on Windows) rather than the
// generic "OS keyring".
func keyringName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS Keychain"
	case "windows":
		return "Windows Credential Manager"
	default:
		return "OS keyring"
	}
}

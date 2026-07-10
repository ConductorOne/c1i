package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var usersGetCmd = &cobra.Command{
	Use:   "get <user-id>",
	Short: "Get a single user by ID (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		data, err := c.Get(cmd.Context(), client.Path("/api/v1/users/%s", args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeObject(cmd, data)
	},
}

func init() {
	usersCmd.AddCommand(usersGetCmd)
}

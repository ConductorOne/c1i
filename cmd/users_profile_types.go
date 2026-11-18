package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var usersProfileTypesCmd = &cobra.Command{
	Use:   "profile-types <user-id>",
	Short: "Show a user's profile types across connected apps (pretty JSON)",
	Long: `Show the profile types associated with a user across their connected apps
(GET /api/v1/users/{user_id}/profile-types), as {"profileTypes": [...]}.
Each profile type has id, name, slug, description, priority, displayToUser
and iconUrl as the API returns them.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Get(cmd.Context(), client.Path("/api/v1/users/%s/profile-types", args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeObject(cmd, data)
	},
}

func init() {
	usersCmd.AddCommand(usersProfileTypesCmd)
}

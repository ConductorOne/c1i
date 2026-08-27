package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var policiesGetCmd = &cobra.Command{
	Use:   "get <policy-id>",
	Short: "Get a single policy by ID (pretty JSON)",
	Long: `Get a single policy by ID.

Unlike "policies list" and the default "policies search", a direct get DOES
return a soft-deleted policy (with deletedAt populated) — it isn't filtered
out here. Verified live: delete a policy, then get it — 200, not 404.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newPoliciesClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		data, err := c.Get(cmd.Context(), client.Path("/api/v1/policies/%s", args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeResource(cmd, data, "id")
	},
}

func init() {
	policiesCmd.AddCommand(policiesGetCmd)
}

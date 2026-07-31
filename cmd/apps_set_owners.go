package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsSetOwnersCmd = &cobra.Command{
	Use:   "set-owners <app-id>",
	Short: "Set the owners of an app (replaces the full owner list)",
	Long: `Set the complete list of owners for an app via PUT .../owners, replacing
any existing owners. Provide one or more --user-id (C1 user IDs, 27 chars each).

Owner changes are provisioned ASYNCHRONOUSLY: this call returns immediately,
but the new owners take up to ~60-90s to show up in "apps get" (appOwners) and
the owners sub-resource. A success here means the request was accepted, not
that the owner list is already live.

Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		userIDs, _ := cmd.Flags().GetStringSlice("user-id")
		if len(userIDs) == 0 {
			return &usageError{fmt.Errorf("at least one --user-id is required")}
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		path := client.Path("/api/v1/apps/%s/owners", args[0])
		body := map[string]any{"userIds": userIDs}
		if dryRunActive() {
			return printDryRun(cmd, "PUT", path, body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		if _, err := c.Put(cmd.Context(), path, body); err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(),
			"Set %d owner(s) on app %s (provisioning is async; allow ~60-90s to appear).\n",
			len(userIDs), args[0])
		return nil
	},
}

func init() {
	appsSetOwnersCmd.Flags().StringSlice("user-id", nil, "C1 user ID to set as owner (repeatable; replaces the full owner list)")
	markRequired(appsSetOwnersCmd, "user-id")
	appsCmd.AddCommand(appsSetOwnersCmd)
}

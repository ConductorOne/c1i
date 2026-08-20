package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var policiesDeleteCmd = &cobra.Command{
	Use:   "delete <policy-id>",
	Short: "Soft-delete a policy",
	Long: `Soft-delete a policy by ID. The row is retained (deletedAt is set) — it is
NOT a hard delete. Verified live: "policies get" still returns the deleted
policy afterward (200, with deletedAt populated) — it is NOT filtered
there. It DOES disappear from "policies list" and the default
"policies search" immediately; "policies search --include-deleted" is the
only way to find it via a listing rather than a direct get. Honors
--dry-run.

A policy referenced by another policy (via a rule's policyId outcome, or as
another policy's baselinePolicyId) cannot be deleted while that reference
exists.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		path := client.Path("/api/v1/policies/%s", args[0])
		if dryRunActive() {
			return printDryRun(cmd, "DELETE", path, nil)
		}

		c, err := newPoliciesClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Delete(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

func init() {
	policiesCmd.AddCommand(policiesDeleteCmd)
}

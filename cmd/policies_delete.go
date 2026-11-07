package cmd

import (
	"net/http"

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
only way to find it via a listing rather than a direct get. Prints {};
exit 0 is the success signal. Honors --dry-run.

A policy referenced by another policy (via a rule's policyId outcome, or as
another policy's baselinePolicyId) cannot be deleted while that reference
exists: the server returns 400 naming the referencing policy ("cannot delete
policy because ..."), so no dangling reference is left behind. Verified
against platform source (assertNotReferenced).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: client.Path("/api/v1/policies/%s", args[0])})
	},
}

func init() {
	markMutating(policiesDeleteCmd)
	policiesCmd.AddCommand(policiesDeleteCmd)
}

package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var accessProfilesGetCmd = &cobra.Command{
	Use:   "get <access-profile-id>",
	Short: "Get a single access profile by ID (pretty JSON)",
	Long: `Get a single access profile by ID.

The API wraps the catalog in requestCatalogView.requestCatalog; the envelope is
unwrapped before printing, so the catalog's own keys (id, displayName,
published, …) are at the top level, beside the view's own siblings
(memberCount, accessEntitlementsPath, createdByUserPath) and the response's
top-level expanded.

A get carries two things "access-profiles list" rows leave out: the catalog's
accessEntitlements — the visibility bindings that decide who can see it, empty
when there are none — and a memberCount, which the list endpoint reports as 0
for every catalog.`,
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

		data, err := c.Get(cmd.Context(), client.Path("/api/v1/catalogs/%s", args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeResource(cmd, data, "id")
	},
}

func init() {
	accessProfilesCmd.AddCommand(accessProfilesGetCmd)
}

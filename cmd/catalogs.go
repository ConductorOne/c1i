package cmd

import "github.com/spf13/cobra"

var catalogsCmd = &cobra.Command{
	Use:   "catalogs",
	Short: "Manage request catalogs (called access profiles in the C1 UI)",
	Long: `Manage request catalogs.

The C1 UI calls these "access profiles"; the API calls them catalogs, and every
path is /api/v1/catalogs. The OpenAPI spec carries both names — its
RequestCatalog schema is tagged "x-speakeasy-entity: Access_Profile".

A catalog groups entitlements a set of users may request, and decides who can
see them: everyone, or only holders of the catalog's access entitlements.`,
}

func init() {
	rootCmd.AddCommand(catalogsCmd)
}

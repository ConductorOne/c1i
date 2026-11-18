package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var appsResourceTypesGetCmd = &cobra.Command{
	Use:   "get <resource-type-id>",
	Short: "Get one of an app's resource types (pretty JSON)",
	Long: `Get one resource type of an app (pretty JSON), unwrapped: the type's own
keys are at the top level and its id is .id. A deleted type is still returned,
with deletedAt set.

An unknown app or resource type exits 4.`,
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
		appID, _ := cmd.Flags().GetString("app-id")
		data, err := c.Get(cmd.Context(), resourceTypePath(appID, args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeResource(cmd, data, "id")
	},
}

func init() {
	addResourceTypeAppFlag(appsResourceTypesGetCmd)
	appsResourceTypesCmd.AddCommand(appsResourceTypesGetCmd)
}

package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsManagedStateGetCmd = &cobra.Command{
	Use:   "get <resource-id>",
	Short: "Get one discovered app's managed state (pretty JSON)",
	Long: `Get the managed state of one discovered app (pretty JSON). <resource-id> is the
resource_id of a "managed-state list" row. The binding's own keys are at the top
level, with its state under "state": {"unmanaged":{}} or
{"managed":{"appId":"<managed-app-id>"}}.

An unknown app, resource type or resource id exits 4.`,
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

		data, err := c.Get(cmd.Context(), managedStateBindingPath(cmd, args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeResource(cmd, data, "resourceId")
	},
}

// managedStateBindingPath addresses one binding by its resource id.
func managedStateBindingPath(cmd *cobra.Command, resourceID string) string {
	appID, _ := cmd.Flags().GetString("app-id")
	resourceTypeID, _ := cmd.Flags().GetString("resource-type-id")
	return client.Path("/api/v1/apps/%s/resource_types/%s/managed_state_bindings/%s", appID, resourceTypeID, resourceID)
}

func init() {
	addManagedStateScopeFlags(appsManagedStateGetCmd)
	appsManagedStateCmd.AddCommand(appsManagedStateGetCmd)
}

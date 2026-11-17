package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsManagedStateCmd = &cobra.Command{
	Use:   "managed-state",
	Short: "Inspect and promote apps a connector discovered",
	Long: `Inspect and promote the apps a connector discovered (for example the apps in an
identity provider's catalog). Each is a resource of the connector app's
"app"-trait resource type; its managed state is "unmanaged" until
"managed-state promote" creates a managed app for it, then "managed" with that
app's id.

The connector app and its resource type are the parent scope, so every
subcommand takes --app-id and --resource-type-id; find the resource type with
"c1i apps resource-types list --app-id <app-id>".`,
}

// managedStateBindingsPath is the collection of managed-state bindings under
// one connector app's resource type.
func managedStateBindingsPath(cmd *cobra.Command) string {
	appID, _ := cmd.Flags().GetString("app-id")
	resourceTypeID, _ := cmd.Flags().GetString("resource-type-id")
	return client.Path("/api/v1/apps/%s/resource_types/%s/managed_state_bindings", appID, resourceTypeID)
}

// addManagedStateScopeFlags registers the parent-scope flags every
// managed-state subcommand shares.
func addManagedStateScopeFlags(cmd *cobra.Command) {
	cmd.Flags().String("app-id", "", "ID of the connector app that discovered the apps")
	cmd.Flags().String("resource-type-id", "", "ID of the connector's resource type for discovered apps")
	markRequired(cmd, "app-id", "resource-type-id")
}

func init() {
	appsCmd.AddCommand(appsManagedStateCmd)
}

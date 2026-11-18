package cmd

import (
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

var appsResourceTypesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a manually managed resource type on an app (pretty JSON)",
	Long: `Create a manually managed resource type on an app via POST
/api/v1/apps/{app-id}/resource_types, then add resources to it with "apps
resources create".

--resource-type is the kind, case-insensitive here, one of the API spec's:
  ` + strings.Join(resourceTypeKinds, ", ") + `
It has no default. Only CUSTOM can repeat on one app: a second resource type of
any other kind fails with a 500 (exit 6, though retrying never helps):
  app resource type already exists
Find the existing one with "c1i apps resource-types list --app-id <app-id>".
The server enforces a --display-name length range; a name outside it is a 400
(exit 2) whose message states the range.

An unknown app exits 4.

Prints the created type, unwrapped like "apps resource-types get": the id is
.id. Honors --dry-run.

Example:
  c1i apps resource-types create --app-id <app-id> --display-name "Payroll role" --resource-type custom`,
	RunE: func(cmd *cobra.Command, args []string) error {
		kind, err := resourceTypeFlag.parse(mustGetString(cmd, "resource-type"))
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		return sendMutation(cmd, mutation{
			Method: http.MethodPost,
			Path:   resourceTypesPath(appID),
			Body:   map[string]any{"displayName": mustGetString(cmd, "display-name"), "resourceType": kind},
		})
	},
}

func init() {
	f := appsResourceTypesCreateCmd.Flags()
	f.String("app-id", "", "ID of the app to create the resource type on")
	f.String("display-name", "", "Display name of the new resource type")
	f.String("resource-type", "", "Kind of resource type: "+resourceTypeFlag.usage())
	markRequired(appsResourceTypesCreateCmd, "app-id", "display-name", "resource-type")
	markMutating(appsResourceTypesCreateCmd)
	appsResourceTypesCmd.AddCommand(appsResourceTypesCreateCmd)
}

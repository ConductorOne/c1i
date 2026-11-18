package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var appsResourceTypesUpdateCmd = &cobra.Command{
	Use:   "update <resource-type-id>",
	Short: "Rename a manually managed resource type (pretty JSON)",
	Long: `Rename a manually managed resource type. The API takes
{"appResourceType": {...}, "updateMask": "..."}; this command builds it.

--display-name is the only field: the API's other writable field, traitIds,
was answered 200 and left unchanged, so this command does not offer it.
The server enforces a --display-name length range; a name outside it is a 400
(exit 2) whose message states the range.

An unknown app or resource type exits 4.

Prints the updated type, unwrapped like "apps resource-types get": the id is
.id. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		obj := map[string]any{}
		mask, err := resourceTypeUpdateSpec.buildUpdateMask(cmd, obj)
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{
			Method: http.MethodPost,
			Path:   resourceTypePath(appID, args[0]),
			Body:   wrapUpdate("appResourceType", obj, mask),
		})
	},
}

var resourceTypeUpdateSpec = &updateSpec{
	fields: []updateField{strUpdate("display-name", "displayName")},
	scope:  []string{"app-id"},
}

func init() {
	addResourceTypeAppFlag(appsResourceTypesUpdateCmd)
	appsResourceTypesUpdateCmd.Flags().String("display-name", "", "New display name ("+displayNameRule+")")
	registerUpdateSpec(appsResourceTypesUpdateCmd, resourceTypeUpdateSpec)
	markMutating(appsResourceTypesUpdateCmd)
	appsResourceTypesCmd.AddCommand(appsResourceTypesUpdateCmd)
}

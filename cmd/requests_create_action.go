package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

// formValuesBody sets formValues from --form-values-file, omitting it when
// the flag is unset.
func formValuesBody(cmd *cobra.Command, body map[string]any) error {
	if file, _ := cmd.Flags().GetString("form-values-file"); file == "" {
		return nil
	}
	values, err := readRequiredJSONObject(cmd, "form-values-file")
	if err != nil {
		return err
	}
	body["formValues"] = values
	return nil
}

const actionIDNotDiscoverable = `The API has no route that lists or creates these actions, so neither c1i
nor "c1i api" can look an id up. An unknown id exits 4 (the server answers
404 "action not found").`

var requestsCreateActionCmd = &cobra.Command{
	Use:   "action",
	Short: "Request a request action (requestable automation)",
	Long: `Create an action task: request one of the tenant's request actions
(requestable automations), POST /api/v1/task/action.

` + actionIDNotDiscoverable + `

--form-values-file is a JSON object sent as formValues. --group-only sends
groupOnly, which the API documents as rejecting the request "with
FAILED_PRECONDITION unless the action is a current group-creation template".

Prints the created task as pretty JSON, unwrapped like "requests get": the id
is .id. --fields is a usage error on a mutation; extract with jq -er, which
fails on a --dry-run preview where jq -r exits 0 with null.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		actionID, _ := cmd.Flags().GetString("action-id")
		body := map[string]any{"actionId": actionID}
		if d, _ := cmd.Flags().GetString("description"); d != "" {
			body["description"] = d
		}
		if g, _ := cmd.Flags().GetBool("group-only"); g {
			body["groupOnly"] = true
		}
		if err := formValuesBody(cmd, body); err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/task/action", Body: body})
	},
}

var requestsCreateResourceActionCmd = &cobra.Command{
	Use:   "resource-action",
	Short: "Request a connector resource-create action, such as a group from a template",
	Long: `Create a task that runs a connector resource-create action, for example
creating a group in a connected app from a group template,
POST /api/v1/task/resource-action.

` + actionIDNotDiscoverable + `

--form-values-file is a JSON object sent as formValues; the API documents
that it is validated against the connector's schema at request time.

Prints the created task as pretty JSON, unwrapped like "requests get": the id
is .id. --fields is a usage error on a mutation; extract with jq -er, which
fails on a --dry-run preview where jq -r exits 0 with null.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		actionID, _ := cmd.Flags().GetString("action-id")
		body := map[string]any{"actionId": actionID}
		if err := formValuesBody(cmd, body); err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/task/resource-action", Body: body})
	},
}

func init() {
	requestsCreateActionCmd.Flags().String("action-id", "", "ID of the request action to run")
	requestsCreateActionCmd.Flags().String("description", "", "Optional description of the request")
	requestsCreateActionCmd.Flags().String("form-values-file", "", `Form values as a JSON object (file, or "-" for stdin)`)
	requestsCreateActionCmd.Flags().Bool("group-only", false, "Refuse unless the action is a current group-creation template")
	allowEmpty(requestsCreateActionCmd, emptyIsOmitted, "description")
	markRequired(requestsCreateActionCmd, "action-id")
	markMutating(requestsCreateActionCmd)
	requestsCreateCmd.AddCommand(requestsCreateActionCmd)

	requestsCreateResourceActionCmd.Flags().String("action-id", "", "ID of the resource-create action to run")
	requestsCreateResourceActionCmd.Flags().String("form-values-file", "", `Form values as a JSON object (file, or "-" for stdin)`)
	markRequired(requestsCreateResourceActionCmd, "action-id")
	markMutating(requestsCreateResourceActionCmd)
	requestsCreateCmd.AddCommand(requestsCreateResourceActionCmd)
}

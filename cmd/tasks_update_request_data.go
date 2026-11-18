package cmd

import "github.com/spf13/cobra"

var tasksUpdateRequestDataAction = taskAction{
	verb: "update-request-data",
	step: stepUnused,
	extraBody: func(cmd *cobra.Command, body map[string]any) error {
		data, err := readRequiredJSONObject(cmd, "data-file")
		if err != nil {
			return err
		}
		body["data"] = data
		return nil
	},
}

var tasksUpdateRequestDataCmd = &cobra.Command{
	Use:   "update-request-data <task-id>",
	Short: "Submit form data for a task at a form step",
	Long: `Submit the request form of a task whose current policy step is a form.

--data-file is a JSON object (file, or "-" for stdin) sent as the request's
data; a missing, malformed, null or non-object file exits 2 before sending.
Its keys are the form's field names (.policy.current.form.form.fields[].name
in "c1i tasks get"). The server validates the object against the form and
names fields by display name, for example:
  request data does not match the required schema (field 'Reason' is required)

The server refuses a task whose current step is not a form with "current
policy step is not a form instance", and a task whose .actions does not list
TASK_ACTION_TYPE_UPDATE_REQUEST_DATA with "action not permitted" (both exit 2).

Prints the task as pretty JSON, unwrapped like "tasks get": the id is .id.
Its .state is the task from before the action, not the outcome; re-read with
"c1i tasks get <task-id>" before acting on it. --fields is a usage error on a
mutation; extract with jq -er, which fails on a --dry-run preview.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksUpdateRequestDataAction.runTaskAction,
}

func init() {
	// No --comment: the UpdateRequestData request carries only data and expandMask.
	tasksUpdateRequestDataCmd.Flags().String("data-file", "", `Form data as a JSON object (file, or "-" for stdin)`)
	annotateRequired(tasksUpdateRequestDataCmd, "data-file")
	markMutating(tasksUpdateRequestDataCmd)
	tasksCmd.AddCommand(tasksUpdateRequestDataCmd)
}

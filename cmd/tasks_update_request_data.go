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
	Long: `Send form data for a task. The API describes it as submitting the request
form of a task at a form step; a successful submission has not been observed
through c1i.

--data-file is a JSON object (file, or "-" for stdin) sent as the request's
data; a missing, malformed, null or non-object file exits 2 before sending.
The form's field names are at .policy.current.form.form.fields[].name in
"c1i tasks get". What was observed:
  - an object that does not fit the form gets a schema error naming fields by
    display name: request data does not match the required schema (field
    'Reason' is required)
  - a valid object on a self-requested task at a form step, whose .actions did
    not list TASK_ACTION_TYPE_UPDATE_REQUEST_DATA, was refused with "action
    not permitted"
  - a task at another step is refused with "current policy step is not a form
    instance"
All three exit 2.

The task is not read first, so <task-id> must be the 27-character id (the
numeric id "tasks get" accepts is rejected by this route).

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

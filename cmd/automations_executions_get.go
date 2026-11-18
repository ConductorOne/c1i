package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var automationsExecutionsGetCmd = &cobra.Command{
	Use:   "get <execution-id>",
	Short: "Get a single automation execution (pretty JSON)",
	Long: `Get one automation execution by its id (the "id" of an "automations executions
list" row, or the executionId "automations execute" prints). The execution is
unwrapped to the top level: .state is its AUTOMATION_EXECUTION_STATE_* value
and .automationTemplateId the automation it belongs to.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getResource(cmd, client.Path("/api/v1/automation_executions/%s", args[0]), nil, "id", "automationExecution")
	},
}

var automationsExecutionsTerminateCmd = &cobra.Command{
	Use:   "terminate <execution-id>",
	Short: "Terminate a running automation execution (pretty JSON)",
	Long: `Terminate a running automation execution. The API describes termination as
asynchronous; confirm it with "automations executions get <execution-id>".
Prints the API's response, which is {}; exit 0 is the success signal. Honors
--dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: client.Path("/api/v1/automation_executions/%s/actions/terminate", args[0]), Body: map[string]any{}})
	},
}

func init() {
	markMutating(automationsExecutionsTerminateCmd)
	automationsExecutionsCmd.AddCommand(automationsExecutionsGetCmd, automationsExecutionsTerminateCmd)
}

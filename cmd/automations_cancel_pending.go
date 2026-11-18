package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var automationsCancelPendingCmd = &cobra.Command{
	Use:   "cancel-pending <automation-id>",
	Short: "Cancel an automation's queued executions (pretty JSON)",
	Long: `Cancel the automation's queued executions that have not started. The API
describes executions already running, and ones started manually, as
unaffected; stop a running one with "automations executions terminate".

Prints the API's response as pretty JSON, {"bulkActionId": "..."}. The API
describes cancellation as asynchronous: exit 0 means accepted, not finished. c1i has no command to read a
bulk action. A tenant without the feature answers 400 "cancelling queued
automation runs is not enabled for this tenant" (exit 2). Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{}
		if reason := mustGetString(cmd, "reason"); reason != "" {
			body["reason"] = reason
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: client.Path("/api/v1/automations/%s/executions/cancel_pending", args[0]), Body: body})
	},
}

func init() {
	automationsCancelPendingCmd.Flags().String("reason", "", "Reason recorded in the system log")
	markMutating(automationsCancelPendingCmd)
	automationsCmd.AddCommand(automationsCancelPendingCmd)
}

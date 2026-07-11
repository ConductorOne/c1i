package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var requestsGetCmd = &cobra.Command{
	Use:   "get <request-id>",
	Short: "Get a single access request by ID (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	Long: `Get a single access request by ID.

Access requests are backed by tasks, so the ID is the task_id returned by
"c1i requests create" or shown in "c1i requests list". The response is the full
task view, including its current policy step and outcome.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		data, err := c.Get(cmd.Context(), client.Path("/api/v1/tasks/%s", args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeObject(cmd, data)
	},
}

func init() {
	requestsCmd.AddCommand(requestsGetCmd)
}

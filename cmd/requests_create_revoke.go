package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var requestsCreateRevokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "Create a revoke access request",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := client.New(cmd.Context(), baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID, _ := cmd.Flags().GetString("app-id")
		entitlementID, _ := cmd.Flags().GetString("entitlement-id")
		userID, _ := cmd.Flags().GetString("user-id")
		description, _ := cmd.Flags().GetString("description")

		body := map[string]any{
			"task": map[string]any{
				"appId":            appID,
				"appEntitlementId": entitlementID,
			},
		}
		taskMap := body["task"].(map[string]any)
		if userID != "" {
			taskMap["userId"] = userID
		}
		if description != "" {
			taskMap["description"] = description
		}

		data, err := c.Post(cmd.Context(), "/api/v1/task/revoke", body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		var resp struct {
			Task struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"task"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Created revoke request: task_id=%s state=%s\n", resp.Task.ID, resp.Task.State)
		return nil
	},
}

func init() {
	requestsCreateRevokeCmd.Flags().String("app-id", "", "Application ID")
	requestsCreateRevokeCmd.Flags().String("entitlement-id", "", "Entitlement ID")
	requestsCreateRevokeCmd.Flags().String("user-id", "", "User ID (defaults to self if omitted)")
	requestsCreateRevokeCmd.Flags().String("description", "", "Justification or description")
	_ = requestsCreateRevokeCmd.MarkFlagRequired("app-id")
	_ = requestsCreateRevokeCmd.MarkFlagRequired("entitlement-id")
	requestsCreateCmd.AddCommand(requestsCreateRevokeCmd)
}

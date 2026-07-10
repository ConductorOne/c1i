package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var accountsSetOwnerCmd = &cobra.Command{
	Use:   "set-owner",
	Short: "Set the identity user (owner) for an app account",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID, _ := cmd.Flags().GetString("app-id")
		appUserID, _ := cmd.Flags().GetString("app-user-id")
		userID, _ := cmd.Flags().GetString("user-id")

		path := client.Path("/api/v1/apps/%s/app_users/%s", appID, appUserID)
		body := map[string]any{
			"appUser": map[string]any{
				"identityUserId": userID,
			},
			"updateMask": "identityUserId",
		}

		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		var resp struct {
			AppUser struct {
				ID             string `json:"id"`
				IdentityUserID string `json:"identityUserId"`
			} `json:"appUser"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Set owner: app_user_id=%s identity_user_id=%s\n", resp.AppUser.ID, resp.AppUser.IdentityUserID)
		return nil
	},
}

func init() {
	accountsSetOwnerCmd.Flags().String("app-id", "", "Application ID")
	accountsSetOwnerCmd.Flags().String("app-user-id", "", "App user (account) ID")
	accountsSetOwnerCmd.Flags().String("user-id", "", "C1 user ID to set as owner")
	markRequired(accountsSetOwnerCmd, "app-id")
	markRequired(accountsSetOwnerCmd, "app-user-id")
	markRequired(accountsSetOwnerCmd, "user-id")
	accountsCmd.AddCommand(accountsSetOwnerCmd)
}

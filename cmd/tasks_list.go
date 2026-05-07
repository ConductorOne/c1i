package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var tasksListCmd = &cobra.Command{
	Use:   "list",
	Short: "Search and list access request tasks (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := client.New(cmd.Context(), baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		query, _ := cmd.Flags().GetString("query")
		state, _ := cmd.Flags().GetString("state")
		assignedToMe, _ := cmd.Flags().GetBool("assigned-to-me")
		pageSize, _ := cmd.Flags().GetInt("page-size")
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")

		var myUserID string
		if assignedToMe {
			data, err := c.Get(cmd.Context(), "/api/v1/auth/introspect", nil)
			if err != nil {
				return fmt.Errorf("failed to get current user: %w", err)
			}
			var introspect struct {
				UserID string `json:"userId"`
			}
			if err := json.Unmarshal(data, &introspect); err != nil {
				return fmt.Errorf("failed to parse introspect response: %w", err)
			}
			myUserID = introspect.UserID
		}

		enc := json.NewEncoder(cmd.OutOrStdout())
		for {
			body := map[string]any{
				"pageSize": pageSize,
			}
			if pageToken != "" {
				body["pageToken"] = pageToken
			}
			if query != "" {
				body["query"] = query
			}
			if state != "" {
				body["taskStates"] = []string{mapTaskState(state)}
			}
			if myUserID != "" {
				body["myWorkUserIds"] = []string{myUserID}
			}

			data, err := c.Post(cmd.Context(), "/api/v1/search/tasks", body)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List []struct {
					Task struct {
						ID              string `json:"id"`
						DisplayName     string `json:"displayName"`
						Description     string `json:"description"`
						State           string `json:"state"`
						UserID          string `json:"userId"`
						CreatedByUserID string `json:"createdByUserId"`
						CreatedAt       string `json:"createdAt"`
						Type            struct {
							Grant *struct {
								AppID            string `json:"appId"`
								AppEntitlementID string `json:"appEntitlementId"`
								Outcome          string `json:"outcome"`
							} `json:"grant"`
							Revoke *struct {
								AppID            string `json:"appId"`
								AppEntitlementID string `json:"appEntitlementId"`
								Outcome          string `json:"outcome"`
							} `json:"revoke"`
							Certify *struct {
								Outcome string `json:"outcome"`
							} `json:"certify"`
						} `json:"type"`
					} `json:"task"`
				} `json:"list"`
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, item := range resp.List {
				t := item.Task
				row := map[string]string{
					"id":                 t.ID,
					"display_name":       t.DisplayName,
					"description":        t.Description,
					"state":              t.State,
					"user_id":            t.UserID,
					"created_by_user_id": t.CreatedByUserID,
					"created_at":         t.CreatedAt,
				}

				switch {
				case t.Type.Grant != nil:
					row["type"] = "grant"
					row["app_id"] = t.Type.Grant.AppID
					row["app_entitlement_id"] = t.Type.Grant.AppEntitlementID
					if o := finalOutcome(t.Type.Grant.Outcome); o != "" {
						row["outcome"] = o
					}
				case t.Type.Revoke != nil:
					row["type"] = "revoke"
					row["app_id"] = t.Type.Revoke.AppID
					row["app_entitlement_id"] = t.Type.Revoke.AppEntitlementID
					if o := finalOutcome(t.Type.Revoke.Outcome); o != "" {
						row["outcome"] = o
					}
				case t.Type.Certify != nil:
					row["type"] = "certify"
					if o := finalOutcome(t.Type.Certify.Outcome); o != "" {
						row["outcome"] = o
					}
				}

				_ = enc.Encode(row)
			}

			if resp.NextPageToken == "" || manualPaging {
				break
			}
			pageToken = resp.NextPageToken
		}

		return nil
	},
}

func init() {
	tasksListCmd.Flags().String("query", "", "Search task display name or description")
	tasksListCmd.Flags().String("state", "", "Filter by state: open, closed")
	tasksListCmd.Flags().Bool("assigned-to-me", false, "Only show tasks assigned to me")
	tasksListCmd.Flags().Int("page-size", 50, "Results per page")
	tasksListCmd.Flags().String("page-token", "", "Pagination cursor")
	tasksCmd.AddCommand(tasksListCmd)
}

func mapTaskState(s string) string {
	switch s {
	case "open":
		return "TASK_STATE_OPEN"
	case "closed":
		return "TASK_STATE_CLOSED"
	default:
		return s
	}
}

// finalOutcome returns the outcome string only when it represents a real
// terminal state. The proto default values (*_OUTCOME_UNSPECIFIED) appear on
// every open task and are noise for agents reading the NDJSON stream.
func finalOutcome(s string) string {
	if s == "" || strings.HasSuffix(s, "_OUTCOME_UNSPECIFIED") {
		return ""
	}
	return s
}

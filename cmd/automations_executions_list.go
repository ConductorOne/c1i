package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var automationsExecutionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List automation executions (NDJSON output)",
	Long: `Stream automation execution history.

Filters --state and --template-id are applied client-side after fetching
each page. The /api/v1/automation_executions endpoint does not currently
support server-side filtering, so a narrow filter still scans every page
the server returns. Combine with --limit to bound the work.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		stateFilter := mapExecutionState(strings.TrimSpace(cmd.Flag("state").Value.String()))
		templateID, _ := cmd.Flags().GetString("template-id")
		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		// --state/--template-id filter client-side, so a fetched row is not
		// necessarily emitted. effectivePageSize tightens based on the emitted
		// count, which would shrink the per-call page toward 1 while paging
		// past non-matching rows (request amplification). Only tighten when no
		// client-side filter is active.
		clientFilter := stateFilter != "" || templateID != ""

		enc := newEmitter(cmd.OutOrStdout())
		emitted := 0
		prevToken := ""
		for !limitReached(emitted, limit) {
			pageSize := requestedPageSize
			if !clientFilter {
				pageSize = effectivePageSize(requestedPageSize, limit, emitted)
			}
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			data, err := c.Get(cmd.Context(), "/api/v1/automation_executions", params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				AutomationExecutions []struct {
					ID                   string `json:"id"`
					AutomationTemplateID string `json:"automationTemplateId"`
					State                string `json:"state"`
					CreatedAt            string `json:"createdAt"`
					CompletedAt          string `json:"completedAt"`
					Duration             string `json:"duration"`
					IsDraft              bool   `json:"isDraft"`
				} `json:"automationExecutions"`
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, e := range resp.AutomationExecutions {
				if stateFilter != "" && e.State != stateFilter {
					continue
				}
				if templateID != "" && e.AutomationTemplateID != templateID {
					continue
				}
				_ = enc.Encode(map[string]any{
					"id":                     e.ID,
					"automation_template_id": e.AutomationTemplateID,
					"state":                  e.State,
					"created_at":             e.CreatedAt,
					"completed_at":           e.CompletedAt,
					"duration":               e.Duration,
					"is_draft":               e.IsDraft,
				})
				emitted++
				if limitReached(emitted, limit) {
					return nil
				}
			}

			if resp.NextPageToken == "" || manualPaging {
				break
			}
			// Some pagination paths on this endpoint have been seen returning
			// the same cursor twice. Catch that and abort rather than spin —
			// the caller can drop --page-token and retry.
			if resp.NextPageToken == prevToken {
				return fmt.Errorf("automation_executions returned the same nextPageToken twice in a row; pagination on this endpoint may be flaky for the current cursor — re-run without --page-token or report the issue")
			}
			prevToken = resp.NextPageToken
			pageToken = resp.NextPageToken
		}

		return nil
	},
}

// mapExecutionState translates the user-friendly --state value to the bare
// enum the API expects. The full enum name (AUTOMATION_EXECUTION_STATE_DONE)
// is verbose; the short form (done|error|pending) is what humans actually
// remember. Case-insensitive. Returns the input unchanged for unknown values
// so a future enum addition surfaces as a server-side validation error
// rather than a silent client-side drop.
func mapExecutionState(s string) string {
	switch strings.ToLower(s) {
	case "":
		return ""
	case "done", "success":
		return "AUTOMATION_EXECUTION_STATE_DONE"
	case "error", "failed", "failure":
		return "AUTOMATION_EXECUTION_STATE_ERROR"
	case "pending":
		return "AUTOMATION_EXECUTION_STATE_PENDING"
	case "creating":
		return "AUTOMATION_EXECUTION_STATE_CREATING"
	case "waiting":
		return "AUTOMATION_EXECUTION_STATE_WAITING"
	case "terminate", "terminated":
		return "AUTOMATION_EXECUTION_STATE_TERMINATE"
	default:
		return s
	}
}

func init() {
	automationsExecutionsListCmd.Flags().String("state", "", "Filter by execution state: done, error, pending, creating, waiting, terminate (or the full AUTOMATION_EXECUTION_STATE_* enum)")
	automationsExecutionsListCmd.Flags().String("template-id", "", "Filter by automation template (the parent automation's ID)")
	automationsExecutionsListCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	automationsExecutionsListCmd.Flags().String("page-token", "", "Pagination cursor")
	addLimitFlag(automationsExecutionsListCmd)
	automationsExecutionsCmd.AddCommand(automationsExecutionsListCmd)
}

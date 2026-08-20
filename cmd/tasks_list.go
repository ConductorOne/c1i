package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

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

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		query, _ := cmd.Flags().GetString("query")
		state, _ := cmd.Flags().GetString("state")
		if err := validateTaskState(state); err != nil {
			return &usageError{err}
		}
		assignedToMe, _ := cmd.Flags().GetBool("assigned-to-me")
		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		var myUserID string
		if assignedToMe {
			myUserID, err = currentUserID(cmd.Context(), c)
			if err != nil {
				return err
			}
			// Guard against silently listing tenant-wide if the current user
			// can't be resolved: --assigned-to-me must narrow to the caller.
			if myUserID == "" {
				return fmt.Errorf("could not determine the current user for --assigned-to-me")
			}
		}

		enc := newEmitter(cmd)
		emitted := 0
		for !limitReached(emitted, limit) {
			pageSize := effectivePageSize(requestedPageSize, limit, emitted)
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

			var resp taskSearchList
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, item := range resp.List {
				_ = enc.Encode(taskRow(item.Task))
				emitted++
				if limitReached(emitted, limit) {
					return nil
				}
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
	tasksListCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	tasksListCmd.Flags().String("page-token", "", "Pagination cursor")
	addLimitFlag(tasksListCmd)
	tasksCmd.AddCommand(tasksListCmd)
}

// validateTaskState rejects a --state value that isn't a recognized filter, so a
// typo fails with a clear usage error instead of a raw gateway 400 on the
// taskStates enum. An empty value means "no state filter" and is allowed. Shared
// by `tasks list` and `requests list`.
func validateTaskState(s string) error {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "open", "closed":
		return nil
	default:
		return fmt.Errorf(`--state must be "open" or "closed"`)
	}
}

// mapTaskState normalizes the user-friendly --state value to the API enum.
// Input is case-insensitive (and surrounding whitespace is ignored) so
// `--state open` and `--state OPEN` both work. Call validateTaskState first to
// reject unrecognized values.
func mapTaskState(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
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

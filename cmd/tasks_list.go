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

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		query, _ := cmd.Flags().GetString("query")
		state, err := parseTaskState(cmd)
		if err != nil {
			return err
		}
		assignedToMe, _ := cmd.Flags().GetBool("assigned-to-me")
		requestedPageSize := pageSizeFlag(cmd)
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
				return &usageError{fmt.Errorf("could not determine the current user for --assigned-to-me")}
			}
		}

		enc := newEmitter(cmd)
		guard := newPaginationCursorGuard()
		route := "POST /api/v1/search/tasks"
		for !limitReached(enc.Written(), limit) {
			if guard.tokenAlreadyUsed(pageToken) {
				return guard.warnStuck(cmd, route)
			}
			pageSize := requestedPageSize
			if !enc.Filtered() {
				pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
			}
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
				body["taskStates"] = []string{state}
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

			keep, err := guard.checkPage(cmd, route, resp.ids())
			if err != nil {
				return err
			}
			for _, item := range keepRows(resp.List, keep) {
				if item.Task == nil {
					continue // skipped rather than emitted as an all-"" row
				}
				if err := enc.Encode(taskRow(*item.Task)); err != nil {
					return err
				}
				if limitReached(enc.Written(), limit) {
					return nil
				}
			}

			if resp.NextPageToken == "" || manualPaging {
				break
			}
			guard.markTokenUsed(pageToken)
			pageToken = resp.NextPageToken
		}

		return nil
	},
}

func init() {
	tasksListCmd.Flags().String("query", "", "Search task display name or description")
	tasksListCmd.Flags().String("state", "", "Filter by state: "+taskStateFlag.usage())
	tasksListCmd.Flags().Bool("assigned-to-me", false, "Only show tasks assigned to me")
	addPaginationFlags(tasksListCmd)
	tasksCmd.AddCommand(tasksListCmd)
}

var taskStateFlag = enumFlag{flag: "state", prefix: "TASK_STATE_", unknown: enumReject, values: []enumValue{
	{name: "open", api: "TASK_STATE_OPEN"},
	{name: "closed", api: "TASK_STATE_CLOSED"},
}}

// parseTaskState reads --state for `tasks list` and `requests list`; blank
// means no filter.
func parseTaskState(cmd *cobra.Command) (string, error) {
	v := mustGetString(cmd, "state")
	if strings.TrimSpace(v) == "" {
		return "", nil
	}
	return taskStateFlag.parse(v)
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

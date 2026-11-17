package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var tasksListCmd = &cobra.Command{
	Use:   "list",
	Short: "Search and list access request tasks (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		query, _ := cmd.Flags().GetString("query")
		state, err := parseTaskState(cmd)
		if err != nil {
			return err
		}
		assignedToMe, _ := cmd.Flags().GetBool("assigned-to-me")
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
		body := map[string]any{}
		if query != "" {
			body["query"] = query
		}
		if state != "" {
			body["taskStates"] = []string{state}
		}
		if myUserID != "" {
			body["myWorkUserIds"] = []string{myUserID}
		}
		return listPages(cmd, c, pageSpec[taskSearchItem]{
			Route: "POST /api/v1/search/tasks",
			Fetch: postPage(cmd.Context(), "/api/v1/search/tasks", body),
			Parse: parseList[taskSearchItem],
			ID:    taskItemID,
			Rows:  taskItemRows,
		})
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

// parseTaskState reads --state for `tasks list` and `requests list`. An empty
// value means no filter; anything else, whitespace-only included, goes to the
// parser, which rejects a blank key rather than dropping the filter.
func parseTaskState(cmd *cobra.Command) (string, error) {
	v := mustGetString(cmd, "state")
	if v == "" {
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

package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

const taskAuditsPath = "/api/v1/task/audits"

var tasksAuditsCmd = &cobra.Command{
	Use:   "audits <task-id>",
	Short: "List a task's audit trail events (NDJSON output)",
	Long: `List the audit trail of one task: creation, approvals, comments, state
changes and provisioning events, oldest first.

Each NDJSON row is one event exactly as the API returns it. Its kind is the
one event key it carries besides the common ones (for example .taskCreated,
.comment, .stateChange, .grantOutcome); .eventType, .currentState, .created
and .userId are on every row.

--newest-first reverses the order. --comments-only keeps only comment events
and --exclude-comments drops them; the two together exit 2 (the server refuses
the pair with "comments_only and exclude_comments are mutually exclusive").

The task is read first: the audit endpoint answers an unknown task id with an
empty list, so c1i exits 4 instead of printing nothing.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		commentsOnly, _ := cmd.Flags().GetBool("comments-only")
		excludeComments, _ := cmd.Flags().GetBool("exclude-comments")
		if commentsOnly && excludeComments {
			return &usageError{fmt.Errorf("--comments-only and --exclude-comments are mutually exclusive")}
		}
		taskID := args[0]
		body := map[string]any{"taskId": taskID}
		if commentsOnly {
			body["commentsOnly"] = true
		}
		if excludeComments {
			body["excludeComments"] = true
		}
		if newest, _ := cmd.Flags().GetBool("newest-first"); newest {
			body["newestFirst"] = true
		}

		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		if _, err := c.Get(cmd.Context(), client.Path("/api/v1/tasks/%s", taskID), nil); err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return listPages(cmd, c, pageSpec[json.RawMessage]{
			Route: "POST " + taskAuditsPath,
			Fetch: postPage(cmd.Context(), taskAuditsPath, body),
			Parse: parseList[json.RawMessage],
			ID:    func(r json.RawMessage) string { return rawItemID(r) },
		})
	},
}

func init() {
	tasksAuditsCmd.Flags().Bool("comments-only", false, "Only comment events")
	tasksAuditsCmd.Flags().Bool("exclude-comments", false, "Drop comment events")
	tasksAuditsCmd.Flags().Bool("newest-first", false, "Newest event first (default: oldest first)")
	addPaginationFlags(tasksAuditsCmd)
	tasksCmd.AddCommand(tasksAuditsCmd)
}

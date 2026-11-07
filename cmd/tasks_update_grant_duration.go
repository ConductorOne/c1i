package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var tasksUpdateGrantDurationAction = taskAction{
	verb: "update-grant-duration",
	step: stepUnused,
	extraBody: func(cmd *cobra.Command, body map[string]any) error {
		duration, _, err := protoDurationFlag(cmd, "duration")
		if err != nil {
			return err
		}
		body["duration"] = duration
		return nil
	},
	confirm: func(id, _, _ string) string {
		return fmt.Sprintf("Updated grant duration: task_id=%s\n", id)
	},
}

var tasksUpdateGrantDurationCmd = &cobra.Command{
	Use:   "update-grant-duration <task-id>",
	Short: "Change the grant duration a task will provision",
	Long: `Change how long the access a grant task provisions will last.

--duration takes a whole-number Go (1h, 1h30m) or protobuf (3600s) duration
(no sign, no decimals) and is sent in seconds, the only form the API parses;
-1h or 1.5h exits 2 before sending. "0s" is sent, and the server has been seen
storing it without complaint.

The task must still be at an approval step. Once it reaches provisioning the
server refuses with:
  cannot update grant duration for a ticket in a provision step

The new value lands on the task as "grantDuration"; read it back with
"c1i requests get <task-id>".

The confirmation reports only the task id: the action endpoints echo the task's
state from before the action.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksUpdateGrantDurationAction.runTaskAction,
}

func init() {
	// No --comment: the UpdateGrantDuration request body carries only duration
	// and expandMask.
	addProtoDurationFlag(tasksUpdateGrantDurationCmd, "duration",
		"Grant duration as a protobuf duration (3600s) or a Go duration (1h), sent in seconds; a sign or decimal (-1h, 1.5h) exits 2")
	markRequired(tasksUpdateGrantDurationCmd, "duration")
	tasksCmd.AddCommand(tasksUpdateGrantDurationCmd)
}

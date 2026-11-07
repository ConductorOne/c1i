package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var tasksUpdateGrantDurationAction = taskAction{
	verb: "update-grant-duration",
	step: stepUnused,
	extraBody: func(cmd *cobra.Command, body map[string]any) error {
		duration, _, err := protoDurationFlag(cmd, "duration", false)
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

--duration takes a Go (1h) or protobuf (3600s) duration and is sent in
seconds, the only form the API parses. It must be positive: the server stores
"0s" without complaint.

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
	tasksUpdateGrantDurationCmd.Flags().String("duration", "",
		"Grant duration as a protobuf duration (3600s) or a Go duration (1h), sent in seconds; must be positive")
	markRequired(tasksUpdateGrantDurationCmd, "duration")
	tasksCmd.AddCommand(tasksUpdateGrantDurationCmd)
}

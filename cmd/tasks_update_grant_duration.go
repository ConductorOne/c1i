package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var tasksUpdateGrantDurationAction = taskAction{
	verb: "update-grant-duration",
	step: stepUnused,
	extraBody: func(cmd *cobra.Command, body map[string]any) error {
		duration, err := requireNonEmptyIfSet(cmd, "duration")
		if err != nil {
			return err
		}
		if duration == "" {
			return &usageError{fmt.Errorf("flag --duration is required; the server rejects a missing one with " +
				`invalid TaskActionsServiceUpdateGrantDurationRequest.Duration: value is required`)}
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

--duration takes a protobuf duration, not a Go one: seconds with an "s"
suffix, e.g. 3600s. "1h" is refused by the server with:
  invalid google.protobuf.Duration value "1h"

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
		`Grant duration as a protobuf duration, e.g. 3600s (required; "1h" is refused)`)
	markRequired(tasksUpdateGrantDurationCmd, "duration")
	tasksCmd.AddCommand(tasksUpdateGrantDurationCmd)
}

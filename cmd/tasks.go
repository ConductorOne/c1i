package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var tasksCmd = &cobra.Command{
	Use:   "tasks",
	Short: "Manage access request tasks",
}

// parseTaskActionResponse extracts the task id and state from a task action
// response (approve/deny/comment). These endpoints nest the updated task under
// taskView.task, not at the top level — the same shape the grant/revoke create
// endpoints return.
func parseTaskActionResponse(data []byte) (id, state string, err error) {
	var resp struct {
		TaskView struct {
			Task struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"task"`
		} `json:"taskView"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", "", err
	}
	// A successful action response always carries the updated task under
	// taskView.task. An empty id means the response wasn't the shape we expect
	// (e.g. the old top-level `task` shape); erroring here prevents silently
	// printing "task_id= state=" — the exact symptom this parsing fixed.
	if resp.TaskView.Task.ID == "" {
		return "", "", fmt.Errorf("unexpected task action response: no taskView.task.id in %s", data)
	}
	return resp.TaskView.Task.ID, resp.TaskView.Task.State, nil
}

func init() {
	rootCmd.AddCommand(tasksCmd)
}

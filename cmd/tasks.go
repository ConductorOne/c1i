package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
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

// parseCurrentPolicyStepID extracts the ID of the currently executing policy
// step from a TaskService.Get response body (taskView.task.policy.current.id).
// The approve/deny endpoints require this ID to indicate which step of a
// multi-step policy the action applies to.
func parseCurrentPolicyStepID(data []byte) (string, error) {
	var resp struct {
		TaskView struct {
			Task struct {
				Policy struct {
					Current struct {
						ID string `json:"id"`
					} `json:"current"`
				} `json:"policy"`
			} `json:"task"`
		} `json:"taskView"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("failed to parse task: %w", err)
	}
	return resp.TaskView.Task.Policy.Current.ID, nil
}

// resolvePolicyStepID returns the policy step ID to use for an approve/deny
// action. If the user supplied one explicitly it is used as-is; otherwise the
// task is fetched and its currently executing step ID is used.
//
// approve requires policyStepId, so callers pass required=true to turn an
// underivable step into an error. deny treats it as optional (the API does
// not require it), so it passes required=false and simply omits the field
// when no current step can be derived.
func resolvePolicyStepID(ctx context.Context, c *client.Client, taskID, explicit string, required bool) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	data, err := c.Get(ctx, fmt.Sprintf("/api/v1/tasks/%s", taskID), nil)
	if err != nil {
		return "", fmt.Errorf("failed to fetch task to determine current policy step: %w", err)
	}
	stepID, err := parseCurrentPolicyStepID(data)
	if err != nil {
		return "", err
	}
	if stepID == "" && required {
		return "", fmt.Errorf("could not determine the current policy step for task %s; pass --policy-step-id explicitly", taskID)
	}
	return stepID, nil
}

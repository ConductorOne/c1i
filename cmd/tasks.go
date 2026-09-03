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

// taskSearchList is the /api/v1/search/tasks response envelope, shared by
// `tasks list` (approver lens) and `requests list` (requester lens).
type taskSearchList struct {
	List          []taskSearchItem `json:"list"`
	NextPageToken string           `json:"nextPageToken"`
}

type taskSearchItem struct {
	Task taskSummary `json:"task"`
}

// taskSummary is the subset of task fields both list views emit.
type taskSummary struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"displayName"`
	Description     string   `json:"description"`
	State           string   `json:"state"`
	UserID          string   `json:"userId"`
	CreatedByUserID string   `json:"createdByUserId"`
	CreatedAt       string   `json:"createdAt"`
	Type            taskType `json:"type"`
}

type taskType struct {
	Grant   *taskGrantRevoke `json:"grant"`
	Revoke  *taskGrantRevoke `json:"revoke"`
	Certify *struct {
		Outcome string `json:"outcome"`
	} `json:"certify"`
}

type taskGrantRevoke struct {
	AppID            string `json:"appId"`
	AppEntitlementID string `json:"appEntitlementId"`
	Outcome          string `json:"outcome"`
}

// taskRow flattens a task into the NDJSON output row shared by the task and
// request list commands. The type-specific fields (app/entitlement/outcome)
// are pulled from whichever of grant/revoke/certify is set.
func taskRow(t taskSummary) map[string]string {
	row := map[string]string{
		"id":                 t.ID,
		"display_name":       t.DisplayName,
		"description":        t.Description,
		"state":              t.State,
		"user_id":            t.UserID,
		"created_by_user_id": t.CreatedByUserID,
		"created_at":         t.CreatedAt,
	}
	switch {
	case t.Type.Grant != nil:
		row["type"] = "grant"
		row["app_id"] = t.Type.Grant.AppID
		row["app_entitlement_id"] = t.Type.Grant.AppEntitlementID
		if o := finalOutcome(t.Type.Grant.Outcome); o != "" {
			row["outcome"] = o
		}
	case t.Type.Revoke != nil:
		row["type"] = "revoke"
		row["app_id"] = t.Type.Revoke.AppID
		row["app_entitlement_id"] = t.Type.Revoke.AppEntitlementID
		if o := finalOutcome(t.Type.Revoke.Outcome); o != "" {
			row["outcome"] = o
		}
	case t.Type.Certify != nil:
		row["type"] = "certify"
		if o := finalOutcome(t.Type.Certify.Outcome); o != "" {
			row["outcome"] = o
		}
	}
	return row
}

// currentUserID resolves the caller's C1 user ID via the introspect endpoint.
// Both `tasks list --assigned-to-me` and `requests list` (default requester
// scope) need it to filter to the current user.
func currentUserID(ctx context.Context, c *client.Client) (string, error) {
	data, err := c.Get(ctx, "/api/v1/auth/introspect", nil)
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}
	var introspect struct {
		UserID string `json:"userId"`
	}
	if err := json.Unmarshal(data, &introspect); err != nil {
		return "", fmt.Errorf("failed to parse introspect response: %w", err)
	}
	return introspect.UserID, nil
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

// resolvePolicyStepID returns the policy step id an action should target:
// the explicit --policy-step-id when given, otherwise the task's currently
// executing step, fetched with a GET.
//
// required distinguishes the two modes callers need. Actions the server
// rejects without a step (approve, skip-step) and those we refuse to send
// ambiguously (reassign, restart) pass true and get an error. deny passes
// false: when the step cannot be derived the field is omitted rather than
// blocking the denial.
func resolvePolicyStepID(ctx context.Context, c *client.Client, taskID, explicit string, required bool) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	data, err := c.Get(ctx, client.Path("/api/v1/tasks/%s", taskID), nil)
	if err != nil {
		if required {
			return "", fmt.Errorf("failed to fetch task to determine current policy step: %w", err)
		}
		return "", nil // optional (deny): can't derive it, so omit the field
	}
	stepID, err := parseCurrentPolicyStepID(data)
	if err != nil {
		if required {
			return "", err
		}
		return "", nil // optional (deny): omit rather than block the action
	}
	if stepID == "" && required {
		return "", &usageError{fmt.Errorf("could not determine the current policy step for task %s; pass --policy-step-id explicitly", taskID)}
	}
	return stepID, nil
}

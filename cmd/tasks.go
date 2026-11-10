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
	Task *taskSummary `json:"task"`
}

// ids gives "" for an item with no task wrapper, so a page of only those reads
// as an unrecognized shape rather than an empty page.
func (l taskSearchList) ids() []string {
	ids := make([]string, len(l.List))
	for i, it := range l.List {
		if it.Task != nil {
			ids[i] = it.Task.ID
		}
	}
	return ids
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
		return "", &nonJSONResponseError{fmt.Errorf("failed to parse introspect response: %w", err)}
	}
	return introspect.UserID, nil
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
// A failed fetch or unparseable body is always an error, required or not: a
// missing task must not look like a task with no step. required only governs
// an empty current step. Actions the server rejects without one (approve,
// skip-step) and reassign, which we refuse to send ambiguously, pass true and
// get a usage error. deny and restart pass false and omit the field, which is
// what lets restart act on a closed task (its GET succeeds; the current step
// is empty).
func resolvePolicyStepID(ctx context.Context, c *client.Client, taskID, explicit string, required bool) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	data, err := c.Get(ctx, client.Path("/api/v1/tasks/%s", taskID), nil)
	if err != nil {
		return "", fmt.Errorf("failed to fetch task to determine current policy step: %w", err)
	}
	stepID, err := parseCurrentPolicyStepID(data)
	if err != nil {
		return "", err
	}
	if stepID == "" && required {
		return "", &usageError{fmt.Errorf("task %s has no current policy step to target; re-read it with \"c1i tasks get %s\" (pass --policy-step-id only if you already know the step)", taskID, taskID)}
	}
	return stepID, nil
}

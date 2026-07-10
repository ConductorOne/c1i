package cmd

import "testing"

// TestParseTaskActionResponse pins the response shape for the task action
// endpoints (approve/deny/comment). The updated task is nested under
// taskView.task, NOT at the top level. A previous version unmarshalled a
// top-level `task` field, which always came back empty — so the commands
// printed task_id= state= with nothing after the =.
func TestParseTaskActionResponse(t *testing.T) {
	body := []byte(`{
		"taskView": {
			"task": {
				"id": "task-123",
				"state": "TASK_STATE_CLOSED"
			}
		},
		"expanded": []
	}`)

	id, state, err := parseTaskActionResponse(body)
	if err != nil {
		t.Fatalf("parseTaskActionResponse returned error: %v", err)
	}
	if id != "task-123" {
		t.Errorf("id = %q, want %q", id, "task-123")
	}
	if state != "TASK_STATE_CLOSED" {
		t.Errorf("state = %q, want %q", state, "TASK_STATE_CLOSED")
	}
}

// TestParseTaskActionResponseTopLevelTaskErrors guards against the old bug: a
// top-level `task` field must NOT be accepted (the real API nests it under
// taskView.task). Rather than silently returning empty id/state — which printed
// "task_id= state=" — this must be a parse error.
func TestParseTaskActionResponseTopLevelTaskErrors(t *testing.T) {
	body := []byte(`{"task": {"id": "wrong", "state": "wrong"}}`)

	if _, _, err := parseTaskActionResponse(body); err == nil {
		t.Error("expected error when taskView.task.id is absent, got nil")
	}
}

func TestParseTaskActionResponseInvalidJSON(t *testing.T) {
	if _, _, err := parseTaskActionResponse([]byte(`{not json`)); err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// TestParseCurrentPolicyStepID pins extraction of the currently executing
// policy step ID (taskView.task.policy.current.id) from a TaskService.Get
// response. Approve requires policyStepId, and deny needs it to target the
// right step on multi-step policies, so when the user omits --policy-step-id
// we derive it from this path.
func TestParseCurrentPolicyStepID(t *testing.T) {
	cases := map[string]struct {
		body    string
		want    string
		wantErr bool
	}{
		"present": {
			body: `{"taskView":{"task":{"policy":{"current":{"id":"step-123"}}}}}`,
			want: "step-123",
		},
		"missing current": {
			body: `{"taskView":{"task":{"policy":{}}}}`,
			want: "",
		},
		"empty object": {
			body: `{}`,
			want: "",
		},
		"malformed": {
			body:    `not json`,
			wantErr: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := parseCurrentPolicyStepID([]byte(tc.body))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseCurrentPolicyStepID(%q) = %q, want error", tc.body, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCurrentPolicyStepID(%q) unexpected error: %v", tc.body, err)
			}
			if got != tc.want {
				t.Errorf("parseCurrentPolicyStepID(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

package cmd

import "testing"

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

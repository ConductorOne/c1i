package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const routingRuleFixture = `{"id":"rr-1","appId":"app-1","displayName":"zz","description":"","priority":2,"enabled":false,"condition":"","settings":{"requestPolicyId":"pol-1","emergencyGrantPolicyId":"","emergencyGrantEnabled":false,"maxGrantDuration":null,"requestSchemaId":""}}`

// TestRoutingRulesListRows: rows keep real JSON types, an empty condition or
// setting is null, and a cursor in the answer warns rather than passing as
// complete (the endpoint documents no paging).
func TestRoutingRulesListRows(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(http.StatusOK, `{"list":[`+routingRuleFixture+`],"nextPageToken":"more","maxRulesPerApp":10}`))
	r := runMutationCLI(t, s.srv, 0, nil, "apps", "routing-rules", "list", "--app-id", "app-1")
	if r.exit != 0 {
		t.Fatalf("exit %d: %s", r.exit, r.stderr)
	}
	if got := s.requests(); len(got) != 1 || got[0].rawPath != "/api/v1/apps/app-1/entitlement_configuration_rules" {
		t.Fatalf("requests = %v", got)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &row); err != nil {
		t.Fatalf("row is not one JSON object: %q", r.stdout)
	}
	want := map[string]any{
		"id": "rr-1", "app_id": "app-1", "display_name": "zz", "description": nil, "priority": float64(2),
		"enabled": false, "condition": nil, "request_policy_id": "pol-1", "emergency_grant_policy_id": nil,
		"emergency_grant_enabled": false, "max_grant_duration": nil, "request_schema_id": nil,
		"created_at": nil, "updated_at": nil,
	}
	for k, v := range want {
		if got, ok := row[k]; !ok || got != v {
			t.Errorf("row[%q] = %#v (present %v), want %#v", k, got, ok, v)
		}
	}
	if len(row) != len(want) {
		t.Errorf("row keys = %v, want %d keys", sortedKeys(row), len(want))
	}
	if !strings.Contains(r.stderr, fixedListWarning) {
		t.Errorf("no partial-result warning for a cursor: stderr %q", r.stderr)
	}
}

// TestRoutingRulesCreateBody pins the create request: settings only when a
// settings flag is given, enabled always explicit, priority only when given.
func TestRoutingRulesCreateBody(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"minimal", nil, `{"displayName":"zz","enabled":false}`},
		{"all", []string{"--description", "d", "--condition", "true", "--enabled", "--priority", "1",
			"--request-policy-id", "pol-1", "--emergency-grant-policy-id", "pol-2", "--emergency-grant-enabled",
			"--max-grant-duration", "8h", "--request-schema-id", "rs-1"},
			`{"condition":"true","description":"d","displayName":"zz","enabled":true,"priority":1,"settings":{"emergencyGrantEnabled":true,"emergencyGrantPolicyId":"pol-2","maxGrantDuration":"28800s","requestPolicyId":"pol-1","requestSchemaId":"rs-1"}}`},
		{"priority zero is sent", []string{"--priority", "0"}, `{"displayName":"zz","enabled":false,"priority":0}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(http.StatusOK, `{"routingRule":`+routingRuleFixture+`}`))
			args := append([]string{"apps", "routing-rules", "create", "--app-id", "app-1", "--display-name", "zz"}, tc.args...)
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != 0 {
				t.Fatalf("exit %d: %s", r.exit, r.stderr)
			}
			got := s.requests()
			if len(got) != 1 || got[0].method != http.MethodPost || got[0].rawPath != "/api/v1/apps/app-1/entitlement_configuration_rules" {
				t.Fatalf("requests = %v", got)
			}
			if got[0].body != tc.want {
				t.Errorf("body = %s\nwant   %s", got[0].body, tc.want)
			}
			var out map[string]any
			if err := json.Unmarshal([]byte(r.stdout), &out); err != nil || out["id"] != "rr-1" {
				t.Errorf("output not unwrapped to the rule: %q", r.stdout)
			}
		})
	}
}

// TestRoutingRulesReorderBody: the ids go in flag order, and the response
// prints as returned.
func TestRoutingRulesReorderBody(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	resp := `{"list":[` + routingRuleFixture + `]}`
	s := newMutServer(t, fixedReply(http.StatusOK, resp))
	r := runMutationCLI(t, s.srv, 0, nil, "apps", "routing-rules", "reorder", "--app-id", "app-1", "--rule-id", "b", "--rule-id", "a")
	if r.exit != 0 {
		t.Fatalf("exit %d: %s", r.exit, r.stderr)
	}
	got := s.requests()
	if len(got) != 1 || got[0].rawPath != "/api/v1/apps/app-1/entitlement_configuration_rules/reorder" || got[0].body != `{"ruleIds":["b","a"]}` {
		t.Fatalf("requests = %v", got)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		t.Fatalf("stdout not JSON: %q", r.stdout)
	}
	if list, ok := out["list"].([]any); !ok || len(list) != 1 {
		t.Errorf("stdout lost the list envelope: %q", r.stdout)
	}
}

// TestUsageControlsGetRefusesUnknownApp: the usage-controls endpoint answers
// an unknown app 200 with null controls, so the command reads the app first.
func TestUsageControlsGetRefusesUnknownApp(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, func(r *http.Request, _ int) mutReply {
		if r.URL.Path == "/api/v1/apps/app-x" {
			return mutReply{status: http.StatusNotFound, body: `{"code":5,"message":"not found"}`}
		}
		return mutReply{body: `{"appUsageControls":null,"hasUsageData":false}`}
	})
	r := runMutationCLI(t, s.srv, 0, nil, "apps", "usage-controls", "get", "app-x")
	if r.exit != exitNotFound {
		t.Errorf("exit %d, want %d (stdout %q, stderr %q)", r.exit, exitNotFound, r.stdout, r.stderr)
	}
	for _, rq := range s.requests() {
		if strings.HasSuffix(rq.rawPath, "/usage_controls") {
			t.Errorf("read usage controls of an unknown app: %v", s.requests())
		}
	}
}

// TestUsageControlsGetShape: set controls are hoisted beside hasUsageData;
// null controls print only hasUsageData.
func TestUsageControlsGetShape(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, tc := range []struct{ name, body, want string }{
		{"null", `{"appUsageControls":null,"hasUsageData":false}`, `{"hasUsageData":false}`},
		{"set", `{"appUsageControls":{"appId":"app-1","notify":true,"notifyAfterDays":30,"revoke":false,"revokeAfterDays":0},"hasUsageData":true}`,
			`{"appId":"app-1","hasUsageData":true,"notify":true,"notifyAfterDays":30,"revoke":false,"revokeAfterDays":0}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, func(r *http.Request, _ int) mutReply {
				if r.URL.Path == "/api/v1/apps/app-1" {
					return mutReply{body: `{"app":{"id":"app-1"}}`}
				}
				return mutReply{body: tc.body}
			})
			r := runMutationCLI(t, s.srv, 0, nil, "apps", "usage-controls", "get", "app-1")
			if r.exit != 0 {
				t.Fatalf("exit %d: %s", r.exit, r.stderr)
			}
			var got any
			if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
				t.Fatalf("stdout not JSON: %q", r.stdout)
			}
			compact, _ := json.Marshal(got)
			if string(compact) != tc.want {
				t.Errorf("stdout = %s, want %s", compact, tc.want)
			}
		})
	}
}

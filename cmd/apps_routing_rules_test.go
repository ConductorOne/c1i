package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// routingRuleRoute answers the app read with a live app, any other GET with
// getBody, and every write with writeBody.
func routingRuleRoute(getBody, writeBody string) func(*http.Request, int) mutReply {
	return func(r *http.Request, _ int) mutReply {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/apps/app-1":
			return mutReply{body: `{"app":{"id":"app-1"}}`}
		case r.Method == http.MethodGet:
			return mutReply{body: getBody}
		}
		return mutReply{body: writeBody}
	}
}

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
			s := newMutServer(t, routingRuleRoute(`{}`, `{"routingRule":`+routingRuleFixture+`}`))
			args := append([]string{"apps", "routing-rules", "create", "--app-id", "app-1", "--display-name", "zz"}, tc.args...)
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != 0 {
				t.Fatalf("exit %d: %s", r.exit, r.stderr)
			}
			got := writes(s)
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
	s := newMutServer(t, routingRuleRoute(`{}`, resp))
	r := runMutationCLI(t, s.srv, 0, nil, "apps", "routing-rules", "reorder", "--app-id", "app-1", "--rule-id", "b", "--rule-id", "a")
	if r.exit != 0 {
		t.Fatalf("exit %d: %s", r.exit, r.stderr)
	}
	got := writes(s)
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

// TestAppScopedCommandsRefuseUnknownOrDeletedApp: the server stores or
// answers for an unknown or soft-deleted app, so each command reads the app
// first: unknown exits 4, deleted exits 2, and nothing past the app read is
// sent, --dry-run included.
func TestAppScopedCommandsRefuseUnknownOrDeletedApp(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	cmds := map[string][]string{
		"create":             {"apps", "routing-rules", "create", "--app-id", "app-x", "--display-name", "zz"},
		"update":             {"apps", "routing-rules", "update", "rr-1", "--app-id", "app-x", "--description", "d"},
		"reorder":            {"apps", "routing-rules", "reorder", "--app-id", "app-x", "--rule-id", "rr-1"},
		"usage-controls get": {"apps", "usage-controls", "get", "app-x"},
	}
	for _, app := range []struct {
		name string
		resp mutReply
		exit int
	}{
		{"unknown", mutReply{status: http.StatusNotFound, body: `{"code":5,"message":"not found"}`}, exitNotFound},
		{"deleted", mutReply{body: `{"app":{"id":"app-x","deletedAt":"2026-01-01T00:00:00Z"}}`}, exitUsage},
	} {
		for name, args := range cmds {
			for _, dry := range []bool{false, true} {
				if dry && name == "usage-controls get" {
					continue
				}
				clearViperOverrides(t, "dry_run")
				s := newMutServer(t, func(r *http.Request, _ int) mutReply {
					if r.URL.Path == "/api/v1/apps/app-x" {
						return app.resp
					}
					return mutReply{body: `{"routingRule":` + routingRuleFixture + `,"appUsageControls":null,"hasUsageData":false}`}
				})
				a := args
				if dry {
					a = append(append([]string{}, args...), "--dry-run")
				}
				r := runMutationCLI(t, s.srv, 0, nil, a...)
				if r.exit != app.exit {
					t.Errorf("%s app, %s dry=%v: exit %d, want %d (stdout %q, stderr %q)", app.name, name, dry, r.exit, app.exit, r.stdout, r.stderr)
				}
				for _, rq := range s.requests() {
					if rq.rawPath != "/api/v1/apps/app-x" {
						t.Errorf("%s app, %s dry=%v: sent %s %s past the app read", app.name, name, dry, rq.method, rq.rawPath)
					}
				}
			}
		}
	}
}

// TestRoutingRulesUpdateKeepsUnchangedFields: the endpoint replaces the whole
// rule, so a flag-only body would wipe every other field; the update sends
// the read rule with only the given flags changed.
func TestRoutingRulesUpdateKeepsUnchangedFields(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	cur := `{"routingRule":{"id":"rr-1","appId":"app-1","displayName":"old","description":"keep me","priority":3,"enabled":true,"condition":"c == 1",` +
		`"settings":{"requestPolicyId":"pol-1","emergencyGrantPolicyId":"pol-2","emergencyGrantEnabled":true,"maxGrantDuration":"3600s","requestSchemaId":"rs-1"},"deletedAt":null}}`
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"display name only", []string{"--display-name", "new"},
			`{"condition":"c == 1","description":"keep me","displayName":"new","enabled":true,"settings":{"emergencyGrantEnabled":true,"emergencyGrantPolicyId":"pol-2","maxGrantDuration":"3600s","requestPolicyId":"pol-1","requestSchemaId":"rs-1"}}`},
		{"one setting", []string{"--max-grant-duration", "8h"},
			`{"condition":"c == 1","description":"keep me","displayName":"old","enabled":true,"settings":{"emergencyGrantEnabled":true,"emergencyGrantPolicyId":"pol-2","maxGrantDuration":"28800s","requestPolicyId":"pol-1","requestSchemaId":"rs-1"}}`},
		{"disable and clear", []string{"--enabled=false", "--condition", ""},
			`{"condition":"","description":"keep me","displayName":"old","enabled":false,"settings":{"emergencyGrantEnabled":true,"emergencyGrantPolicyId":"pol-2","maxGrantDuration":"3600s","requestPolicyId":"pol-1","requestSchemaId":"rs-1"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, routingRuleRoute(cur, cur))
			args := append([]string{"apps", "routing-rules", "update", "rr-1", "--app-id", "app-1"}, tc.args...)
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != 0 {
				t.Fatalf("exit %d: %s", r.exit, r.stderr)
			}
			w := writes(s)
			if len(w) != 1 || w[0].method != http.MethodPost || w[0].rawPath != "/api/v1/apps/app-1/entitlement_configuration_rules/rr-1" {
				t.Fatalf("writes = %v", w)
			}
			if w[0].body != tc.want {
				t.Errorf("body = %s\nwant   %s", w[0].body, tc.want)
			}
		})
	}
}

// TestRoutingRulesUpdateRefusesDeletedRule: the server answers an update of a
// deleted rule 400; c1i refuses it from the read, --dry-run included.
func TestRoutingRulesUpdateRefusesDeletedRule(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, dry := range []bool{false, true} {
		clearViperOverrides(t, "dry_run")
		s := newMutServer(t, routingRuleRoute(`{"routingRule":{"id":"rr-1","displayName":"zz","settings":{},"deletedAt":"2026-01-01T00:00:00Z"}}`, `{}`))
		args := []string{"apps", "routing-rules", "update", "rr-1", "--app-id", "app-1", "--description", "d"}
		if dry {
			args = append(args, "--dry-run")
		}
		r := runMutationCLI(t, s.srv, 0, nil, args...)
		if r.exit != exitUsage || !strings.Contains(r.stderr, "deleted") {
			t.Errorf("dry=%v: exit %d, stderr %q; want %d naming the deleted rule", dry, r.exit, r.stderr, exitUsage)
		}
		if w := writes(s); len(w) != 0 {
			t.Errorf("dry=%v: sent %v", dry, w)
		}
	}
}

// TestRoutingRulesUpdateNothingToUpdate: no field flag is a usage error from
// the update table, before any request.
func TestRoutingRulesUpdateNothingToUpdate(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, routingRuleRoute(`{}`, `{}`))
	r := runMutationCLI(t, s.srv, 0, nil, "apps", "routing-rules", "update", "rr-1", "--app-id", "app-1")
	if r.exit != exitUsage || !strings.Contains(r.stderr, "nothing to update") {
		t.Errorf("exit %d, stderr %q", r.exit, r.stderr)
	}
	if n := len(s.requests()); n != 0 {
		t.Errorf("sent %d requests", n)
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

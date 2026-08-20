package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPolicyRowDeletedAtIsNullNotEmptyString pins that a live policy's
// deleted_at value is the untyped nil (JSON null), not "". An empty string
// is truthy in jq, so `jq 'select(.deleted_at)'` would match every row,
// silently defeating the flag meant to surface only deleted policies.
func TestPolicyRowDeletedAtIsNullNotEmptyString(t *testing.T) {
	tests := []struct {
		name      string
		deletedAt string
		want      any
	}{
		{name: "live policy", deletedAt: "", want: nil},
		{name: "deleted policy", deletedAt: "2024-01-02T03:04:05Z", want: "2024-01-02T03:04:05Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := policyRow(policyListItem{ID: "p1", DeletedAt: tt.deletedAt})

			got, ok := row["deleted_at"]
			if !ok {
				t.Fatal("row has no deleted_at key")
			}

			if tt.want == nil {
				// Compare against untyped nil specifically: an == nil check
				// on an any holding "" would be false anyway, but a plain
				// falsy/zero-value assertion could still let "" slip through
				// in a careless refactor. Require exactly nil.
				if got != nil {
					t.Fatalf("deleted_at = %#v (%T), want untyped nil", got, got)
				}
				return
			}

			gotStr, ok := got.(string)
			if !ok {
				t.Fatalf("deleted_at has type %T, want string", got)
			}
			if gotStr != tt.want {
				t.Errorf("deleted_at = %q, want %q", gotStr, tt.want)
			}
		})
	}
}

// TestPolicyRowKeepsRealJSONTypes pins the type contract CLAUDE.md calls out
// as having recurred across six row builders: system_builtin is a bool and
// rule_count/step_count are numeric, never stringified. A string here would
// make `jq 'select(.stable)')`-style truthiness checks match every row and
// break numeric comparisons like `jq 'select(.tool_count > 5)'`.
func TestPolicyRowKeepsRealJSONTypes(t *testing.T) {
	item := policyListItem{
		ID:            "p1",
		SystemBuiltin: true,
		Rules: []struct {
			Condition string `json:"condition"`
		}{{Condition: "true"}, {Condition: "false"}},
		PolicySteps: map[string]struct {
			Steps []json.RawMessage `json:"steps"`
		}{
			"grant": {Steps: []json.RawMessage{json.RawMessage(`{}`), json.RawMessage(`{}`)}},
		},
	}
	row := policyRow(item)

	switch v := row["system_builtin"].(type) {
	case bool:
		if !v {
			t.Errorf("system_builtin = false, want true")
		}
	default:
		t.Errorf("system_builtin has type %T, want bool", v)
	}

	switch v := row["rule_count"].(type) {
	case int:
		if v != 2 {
			t.Errorf("rule_count = %d, want 2", v)
		}
	default:
		t.Errorf("rule_count has type %T, want int", v)
	}

	switch v := row["step_count"].(type) {
	case int:
		if v != 2 {
			t.Errorf("step_count = %d, want 2", v)
		}
	default:
		t.Errorf("step_count has type %T, want int", v)
	}
}

// TestPolicyRowJSONRendersNullDeletedAt is the property a jq consumer
// actually depends on: serialized through encoding/json, a live policy's row
// must render the literal `"deleted_at":null`, not `"deleted_at":""`.
func TestPolicyRowJSONRendersNullDeletedAt(t *testing.T) {
	row := policyRow(policyListItem{ID: "p1"})

	b, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if !strings.Contains(string(b), `"deleted_at":null`) {
		t.Errorf("marshaled row = %s, want it to contain \"deleted_at\":null", b)
	}
	if strings.Contains(string(b), `"deleted_at":""`) {
		t.Errorf(`marshaled row = %s, deleted_at rendered as empty string instead of null`, b)
	}
}

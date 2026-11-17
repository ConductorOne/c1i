package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

// The two finding-rule list commands share most of their row, but the API
// objects differ: a routing rule has an integer priority and no
// evaluationOrder, while a transformation rule has evaluationOrder (a
// sequence, not a rank) and no priority at all. A shared builder that always
// mapped priority emitted a null column on transformation rows; these two
// canned responses pin each row's key set so it can't come back.
const (
	routingRuleListBody = `{"list":[{"id":"rr-1","appId":"app-1","displayName":"Escalate","description":"d","priority":7,"enabled":true,"findingType":"FINDING_TYPE_CUSTOM","condition":"true","action":{"createTask":{"policyId":"p1"}}}]}`
	// priority is present here only to prove it is not surfaced: the API never
	// sends it on a transformation rule.
	transformationRuleListBody = `{"list":[{"id":"tr-1","appId":"app-1","displayName":"Reweight","description":"d","enabled":false,"findingType":"FINDING_TYPE_CUSTOM","evaluationOrder":3,"condition":"true","transforms":[{"setSeverity":"HIGH"},{"setState":"OPEN"}],"priority":9}]}`
)

// stubFindingRuleList serves body to every request.
func stubFindingRuleList(t *testing.T, body string) {
	t.Helper()
	t.Setenv("C1I_URL", mutTestURL)
	clearViperOverrides(t, "fields") // another test may have left an override
	s := newMutServer(t, fixedReply(200, body))
	stubNewClient(t, s.srv)
}

func findingRuleListRow(t *testing.T, stdout string) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 1 || lines[0] == "" {
		t.Fatalf("stdout = %q, want exactly one NDJSON row", stdout)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &row); err != nil {
		t.Fatalf("decoding row %q: %v", lines[0], err)
	}
	return row
}

func TestFindingRuleListRowColumns(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		body  string
		want  []string
		check func(*testing.T, map[string]any)
	}{
		{
			name: "routing rules keep priority",
			args: []string{"findings", "routing-rules", "list"},
			body: routingRuleListBody,
			want: []string{"app_id", "description", "display_name", "enabled", "finding_type", "id", "priority"},
			check: func(t *testing.T, row map[string]any) {
				if row["priority"] != float64(7) {
					t.Errorf("priority = %#v, want the integer 7", row["priority"])
				}
			},
		},
		{
			name: "transformation rules have evaluation_order, not priority",
			args: []string{"findings", "transformation-rules", "list"},
			body: transformationRuleListBody,
			want: []string{"app_id", "description", "display_name", "enabled", "evaluation_order", "finding_type", "id", "transform_count"},
			check: func(t *testing.T, row map[string]any) {
				if _, ok := row["priority"]; ok {
					t.Errorf("priority = %#v: transformation rules have no priority field", row["priority"])
				}
				if row["evaluation_order"] != float64(3) {
					t.Errorf("evaluation_order = %#v, want 3", row["evaluation_order"])
				}
				if row["transform_count"] != float64(2) {
					t.Errorf("transform_count = %#v, want 2", row["transform_count"])
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubFindingRuleList(t, tc.body)
			r := runCLI(t, nil, nil, tc.args...)
			if r.err != nil {
				t.Fatalf("exit %d: %v", r.exit, r.err)
			}
			row := findingRuleListRow(t, r.stdout)
			if got := sortedKeys(row); strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("keys = %v, want %v", got, tc.want)
			}
			tc.check(t, row)
		})
	}
}

// Those keys are the row's real contract, so --fields must find them (and
// must not pretend a transformation rule has a priority to project).
func TestFindingRuleListFieldsMatchRowKeys(t *testing.T) {
	t.Run("transformation evaluation_order", func(t *testing.T) {
		stubFindingRuleList(t, transformationRuleListBody)
		r := runCLI(t, nil, nil, "findings", "transformation-rules", "list", "--fields", "evaluation_order")
		if r.err != nil {
			t.Fatalf("exit %d: %v", r.exit, r.err)
		}
		if row := findingRuleListRow(t, r.stdout); len(row) != 1 || row["evaluation_order"] != float64(3) {
			t.Errorf("row = %v, want only evaluation_order", row)
		}
	})
	t.Run("transformation priority matches nothing", func(t *testing.T) {
		stubFindingRuleList(t, transformationRuleListBody)
		r := runCLI(t, nil, nil, "findings", "transformation-rules", "list", "--fields", "priority")
		if r.exit != exitUsage {
			t.Errorf("exit = %d (%v), want %d: no transformation row has a priority key", r.exit, r.err, exitUsage)
		}
	})
	t.Run("routing priority", func(t *testing.T) {
		stubFindingRuleList(t, routingRuleListBody)
		r := runCLI(t, nil, nil, "findings", "routing-rules", "list", "--fields", "priority")
		if r.err != nil {
			t.Fatalf("exit %d: %v", r.exit, r.err)
		}
		if row := findingRuleListRow(t, r.stdout); len(row) != 1 || row["priority"] != float64(7) {
			t.Errorf("row = %v, want only priority", row)
		}
	})
}

package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// Real wire payloads captured from GET /api/v1/policies. These two policies
// are the case that motivated step_kinds: on a live tenant they agree on
// every row key that existed before it (grant, system-builtin, one step, no
// rules) and differ only in the step's kind.
const (
	autoApprovalPolicyJSON = `{"id":"auto","displayName":"Auto-approval","description":"Immediately grant access (no approval required)","systemBuiltin":true,"policyType":"POLICY_TYPE_GRANT","rules":[],"policySteps":{"grant":{"steps":[{"accept":{"acceptMessage":""}}]}}}`
	appOwnerApprovalJSON   = `{"id":"gate","displayName":"App owner approval","description":"Assign to app owner for approval","systemBuiltin":true,"policyType":"POLICY_TYPE_GRANT","rules":[],"policySteps":{"grant":{"steps":[{"approval":{"appOwners":{"allowSelfApproval":true}}}]}}}`

	// A policy with conditional routing: the baseline auto-approves, and a
	// rule routes high-risk requests to a UUID-keyed alternative sequence.
	// Only one sequence ever runs. The UUID key deliberately starts with a
	// digit, which sorts before "grant" in ASCII — the ordering hazard that
	// makes flattening the map wrong as well as semantically wrong. Kept
	// all-zeros on purpose: a high-entropy UUID here trips gitleaks'
	// generic-api-key rule in CI, and only the leading digit matters.
	conditionalBranchPolicyJSON = `{"id":"branch","displayName":"Auto-approval","description":"Auto-approve unless high risk","systemBuiltin":true,"policyType":"POLICY_TYPE_GRANT","rules":[{"condition":"request.risk == 'high'","stepKey":"00000000-0000-4000-8000-000000000001"}],"policySteps":{"grant":{"steps":[{"accept":{}}]},"00000000-0000-4000-8000-000000000001":{"steps":[{"approval":{}},{"accept":{}}]}}}`
)

// stepKindsOf asserts a row's step_kinds is a non-nil []string and returns it.
// Discarding the comma-ok and checking only len()==0 lets both a nil slice and
// a changed type pass silently, and a nil slice marshals to null rather than
// the [] the row contract promises.
func stepKindsOf(t *testing.T, row map[string]any) []string {
	t.Helper()
	got, ok := row["step_kinds"].([]string)
	if !ok {
		t.Fatalf("step_kinds has type %T, want []string", row["step_kinds"])
	}
	if got == nil {
		t.Fatal("step_kinds is a nil []string; it must be non-nil so it marshals to [] and not null")
	}
	return got
}

// policyRowFromJSON decodes a raw API policy object the way list/search do,
// then builds its NDJSON row — so these tests exercise the decode path too,
// not just policyRow on a hand-built struct.
func policyRowFromJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var item policyListItem
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("unmarshaling policy: %v", err)
	}
	return policyRow(item)
}

// TestPolicyRowStepKindsSeparatesAutoApprovalFromApprovalGate is the
// load-bearing test. A field toolkit needed the tenant's auto-approval grant
// policy, could not identify it from the row keys, and matched on the English
// display name instead — which is not portable ("Auto-approval" in one tenant,
// "Auto approval" in the next). This pins that every key step_kinds was added
// alongside agrees between the two, so the test fails if step_kinds stops
// being what separates them. (id/display_name/description differ, of course;
// they are what the caller must NOT have to match on.)
func TestPolicyRowStepKindsSeparatesAutoApprovalFromApprovalGate(t *testing.T) {
	auto := policyRowFromJSON(t, autoApprovalPolicyJSON)
	gate := policyRowFromJSON(t, appOwnerApprovalJSON)

	for _, key := range []string{"policy_type", "system_builtin", "step_count", "rule_count", "deleted_at"} {
		if !reflect.DeepEqual(auto[key], gate[key]) {
			t.Fatalf("premise broken: %s differs (%#v vs %#v); these policies are supposed to be indistinguishable without step_kinds", key, auto[key], gate[key])
		}
	}

	if got, want := auto["step_kinds"], []string{"accept"}; !reflect.DeepEqual(got, want) {
		t.Errorf("auto-approval step_kinds = %#v, want %#v", got, want)
	}
	if got, want := gate["step_kinds"], []string{"approval"}; !reflect.DeepEqual(got, want) {
		t.Errorf("approval-gate step_kinds = %#v, want %#v", got, want)
	}
	if reflect.DeepEqual(auto["step_kinds"], gate["step_kinds"]) {
		t.Fatal("step_kinds does not separate an auto-approval policy from an approval gate")
	}
}

// publishedSelectorClauses are the jq clauses `policies list --help` prints as
// the way to identify a tenant's auto-approval policy. autoApprovalSelector
// evaluates exactly these, and TestPublishedSelectorMatchesHelpText asserts the
// help text still contains every one -- without that assertion a clause can be
// dropped from the shipped recipe and no test notices, which is what happened
// when the fourth clause was added.
var publishedSelectorClauses = []string{
	`.policy_type=="POLICY_TYPE_GRANT"`,
	`.system_builtin`,
	`.step_kinds==["accept"]`,
	`.baseline_policy_id==null`,
}

// autoApprovalSelector mirrors that recipe, evaluated against the marshaled
// row so it tests what a caller actually pipes into jq. The baseline_policy_id
// clause cannot change the outcome on its own -- a delegating policy has no
// baseline entry, so step_kinds is [] and the ==["accept"] test already
// excludes it -- but it is evaluated here because the published string
// evaluates it.
func autoApprovalSelector(t *testing.T, raw string) bool {
	t.Helper()
	b, err := json.Marshal(policyRowFromJSON(t, raw))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var row struct {
		PolicyType       string   `json:"policy_type"`
		SystemBuiltin    bool     `json:"system_builtin"`
		StepKinds        []string `json:"step_kinds"`
		BaselinePolicyID *string  `json:"baseline_policy_id"`
	}
	if err := json.Unmarshal(b, &row); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return row.PolicyType == "POLICY_TYPE_GRANT" && row.SystemBuiltin &&
		reflect.DeepEqual(row.StepKinds, []string{"accept"}) &&
		row.BaselinePolicyID == nil
}

// selectBody returns the contents of the first balanced select(...) in s whose
// body mentions want, with all whitespace runs collapsed to single spaces so a
// wrapped help string compares equal to a one-line one. ok is false when there
// is no such call or its parentheses never balance — an unbalanced recipe is
// invalid jq, so that must fail rather than quietly match.
func selectBody(s, want string) (string, bool) {
	for i := 0; ; {
		j := strings.Index(s[i:], "select(")
		if j < 0 {
			return "", false
		}
		open := i + j + len("select(")
		depth := 1
		for k := open; k < len(s); k++ {
			switch s[k] {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 {
				body := strings.Join(strings.Fields(s[open:k]), " ")
				if strings.Contains(body, want) {
					return body, true
				}
				i = k
				break
			}
			if k == len(s)-1 {
				return "", false
			}
		}
		if depth != 0 {
			return "", false
		}
	}
}

// TestPublishedSelectorMatchesHelpText ties the shipped help text to the
// selector these tests exercise. It compares the whole select(...) body, not
// four free-floating substrings: matching on substrings alone would pass an
// "and" rewritten to "or", an extra clause, a reordering, or a dropped closing
// paren, all of which change or invalidate what a reader runs.
func TestPublishedSelectorMatchesHelpText(t *testing.T) {
	// A truncated clause list would make every assertion below vacuous.
	if len(publishedSelectorClauses) != 4 {
		t.Fatalf("publishedSelectorClauses has %d entries, want 4; emptying it would silently disable this guard", len(publishedSelectorClauses))
	}

	body, ok := selectBody(policiesListCmd.Long, ".step_kinds")
	if !ok {
		t.Fatal("policies list --help has no balanced select(...) mentioning .step_kinds; the published recipe is missing or its parentheses do not balance")
	}

	want := strings.Join(publishedSelectorClauses, " and ")
	if body != want {
		t.Errorf("published recipe and tested selector have drifted apart.\n help text: select(%s)\n  expected: select(%s)\nUpdate the help text, publishedSelectorClauses and autoApprovalSelector together.", body, want)
	}
}

// threeClauseSelector is the published recipe minus its baseline_policy_id
// clause, so the two tests below can measure exactly what that clause changes.
func threeClauseSelector(t *testing.T, raw string) bool {
	t.Helper()
	b, err := json.Marshal(policyRowFromJSON(t, raw))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var row struct {
		PolicyType    string   `json:"policy_type"`
		SystemBuiltin bool     `json:"system_builtin"`
		StepKinds     []string `json:"step_kinds"`
	}
	if err := json.Unmarshal(b, &row); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return row.PolicyType == "POLICY_TYPE_GRANT" && row.SystemBuiltin &&
		reflect.DeepEqual(row.StepKinds, []string{"accept"})
}

// TestDelegatingClauseAgreesOnSchemaConformantRows pins the docs' claim that
// the baseline_policy_id clause states an intent rather than changing the
// result: on any row a conforming server can send, a delegating policy has no
// baseline entry, so step_kinds is [] and ==["accept"] already excludes it.
//
// builtinDelegatingPolicyJSON is what makes this measure anything. Every other
// delegating fixture is system_builtin:false, so both selectors short-circuit
// before step_kinds and would agree under any implementation at all.
func TestDelegatingClauseAgreesOnSchemaConformantRows(t *testing.T) {
	// Guard the guard: the delegating fixture must actually reach the
	// step_kinds test rather than being dropped on system_builtin.
	if !strings.Contains(builtinDelegatingPolicyJSON, `"systemBuiltin":true`) {
		t.Fatal("builtinDelegatingPolicyJSON must be system-builtin, or this test short-circuits and proves nothing")
	}
	for _, raw := range []string{
		autoApprovalPolicyJSON,
		appOwnerApprovalJSON,
		conditionalBranchPolicyJSON,
		delegatingPolicyJSON,
		builtinDelegatingPolicyJSON,
		`{"id":"broken","policyType":"POLICY_TYPE_GRANT","systemBuiltin":true,"policySteps":{},"rules":[]}`,
	} {
		four, three := autoApprovalSelector(t, raw), threeClauseSelector(t, raw)
		if four != three {
			t.Errorf("policy %s: four-clause selector = %v, three-clause = %v; on a schema-conformant row they must agree, since baseline_policy_id and a baseline entry are mutually exclusive", raw, four, three)
		}
	}
}

// TestDelegatingClauseGuardsBothFieldsSet is the case where the clause is
// load-bearing, and the reason it is published rather than dropped as
// redundant. A response setting baselinePolicyId AND a baseline entry violates
// the schema, but decodes cleanly and nothing here rejects it; without the
// clause such a row is reported as the tenant's auto-approval policy when its
// baseline is actually deferred elsewhere.
func TestDelegatingClauseGuardsBothFieldsSet(t *testing.T) {
	if !threeClauseSelector(t, bothSetPolicyJSON) {
		t.Fatal("premise broken: the three-clause selector must match the both-set row, or this test is not measuring the clause")
	}
	if autoApprovalSelector(t, bothSetPolicyJSON) {
		t.Error("published selector matched a row whose baseline is deferred elsewhere; the baseline_policy_id clause is what must exclude it")
	}
}

// TestPolicyRowStepKindsJQSelector reproduces the exact selection the field
// needed: the published selector picks the auto-approval policy and nothing
// else.
func TestPolicyRowStepKindsJQSelector(t *testing.T) {
	if !autoApprovalSelector(t, autoApprovalPolicyJSON) {
		t.Error("selector did not match the auto-approval policy")
	}
	if autoApprovalSelector(t, appOwnerApprovalJSON) {
		t.Error("selector also matched the approval gate; it must not")
	}
	// A baseline that gates before accepting is a gate, not auto-approval.
	twoStep := `{"id":"two","policyType":"POLICY_TYPE_GRANT","systemBuiltin":true,"policySteps":{"grant":{"steps":[{"approval":{}},{"accept":{}}]}}}`
	if autoApprovalSelector(t, twoStep) {
		t.Error("selector matched an approval->accept baseline; only a lone accept step is auto-approval")
	}
}

// TestPolicyStepKindsReportsBaselineNotConditionalBranches is the regression
// this file exists for after the wire model was corrected. policySteps holds
// one baseline entry (keyed by the lowercased policy type) plus any number of
// UUID-keyed alternative sequences that `rules` routes to conditionally; only
// one sequence ever executes. Flattening them all together made an
// auto-approving policy with one conditional branch emit
// ["approval","accept"], so the published selector silently missed it — the
// exact failure class this change exists to eliminate.
func TestPolicyStepKindsReportsBaselineNotConditionalBranches(t *testing.T) {
	row := policyRowFromJSON(t, conditionalBranchPolicyJSON)

	if got, want := row["step_kinds"], []string{"accept"}; !reflect.DeepEqual(got, want) {
		t.Errorf("step_kinds = %#v, want %#v (the baseline sequence only)", got, want)
	}
	if !autoApprovalSelector(t, conditionalBranchPolicyJSON) {
		t.Error("the published selector missed an auto-approving policy that has a conditional branch")
	}
	// step_count still counts every sequence, so the two disagree here by
	// design; rule_count is the signal that alternatives exist.
	if got := row["step_count"]; got != 3 {
		t.Errorf("step_count = %v, want 3 (baseline + branch)", got)
	}
	if got := row["rule_count"]; got != 1 {
		t.Errorf("rule_count = %v, want 1", got)
	}
}

// TestPolicyStepKindsIgnoresBranchKeySortOrder pins that no conditional
// branch can lead the array. UUID keys beginning with a digit sort before
// every lowercased policy type, so a map-flattening implementation puts a
// branch step first and reports a sequence that never executes as written.
func TestPolicyStepKindsIgnoresBranchKeySortOrder(t *testing.T) {
	// Branch keys chosen to bracket "grant" alphabetically on both sides.
	raw := `{"policyType":"POLICY_TYPE_GRANT","policySteps":{"0aaa-branch":{"steps":[{"reject":{}}]},"zzz-branch":{"steps":[{"wait":{}}]},"grant":{"steps":[{"accept":{}},{"form":{}}]}}}`

	want := []string{"accept", "form"}
	// Repeat: the baseline lookup must not depend on Go's randomized map
	// iteration order, and no branch may leak in from either side.
	for i := 0; i < 200; i++ {
		got := stepKindsOf(t, policyRowFromJSON(t, raw))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("step_kinds = %#v, want %#v (baseline only, in step order)", got, want)
		}
	}
}

// TestPolicyBaselineKey pins the derivation against every policy type the
// enum defines, since an unrecognized key silently empties step_kinds.
func TestPolicyBaselineKey(t *testing.T) {
	for _, tt := range []struct{ policyType, want string }{
		{"POLICY_TYPE_GRANT", "grant"},
		{"POLICY_TYPE_REVOKE", "revoke"},
		{"POLICY_TYPE_CERTIFY", "certify"},
		{"POLICY_TYPE_ACCESS_REQUEST", "access_request"},
		{"POLICY_TYPE_PROVISION", "provision"},
		{"POLICY_TYPE_UNSPECIFIED", "unspecified"},
		{"", ""},
	} {
		if got := policyBaselineKey(tt.policyType); got != tt.want {
			t.Errorf("policyBaselineKey(%q) = %q, want %q", tt.policyType, got, tt.want)
		}
	}
}

// TestPolicyStepKindsMissingBaselineFailsClosed covers a policy whose
// baseline key isn't present — an unset or unrecognized policy type. Guessing
// a sequence would be worse than reporting none: an empty list cannot match a
// kind-specific selector.
func TestPolicyStepKindsMissingBaselineFailsClosed(t *testing.T) {
	for _, raw := range []string{
		// No policyType at all, so no baseline key can be derived.
		`{"id":"notype","policySteps":{"grant":{"steps":[{"accept":{}}]}}}`,
		// Policy type present but its baseline entry is absent; only a
		// conditional branch is populated.
		`{"id":"nobaseline","policyType":"POLICY_TYPE_GRANT","policySteps":{"0f8a-branch":{"steps":[{"accept":{}}]}}}`,
	} {
		row := policyRowFromJSON(t, raw)
		got := stepKindsOf(t, row)
		if len(got) != 0 {
			t.Errorf("policy %s: step_kinds = %#v, want empty", raw, got)
		}
		if autoApprovalSelector(t, raw) {
			t.Errorf("policy %s: selector matched a policy whose baseline could not be identified", raw)
		}
	}
}

// TestPolicyRowStepKindsIsAlwaysAJSONArray pins that a policy with no baseline
// steps renders "step_kinds":[] and not "step_kinds":null. A nil []string
// marshals to null, which would make `.step_kinds | index("accept")` behave
// differently across rows and break the array type consumers rely on.
func TestPolicyRowStepKindsIsAlwaysAJSONArray(t *testing.T) {
	for _, raw := range []string{
		`{"id":"nosteps"}`,
		`{"id":"emptymap","policyType":"POLICY_TYPE_GRANT","policySteps":{}}`,
		`{"id":"emptysteps","policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[]}}}`,
		`{"id":"nullsteps","policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":null}}}`,
		`{"id":"branchonly","policyType":"POLICY_TYPE_GRANT","policySteps":{"0f8a-branch":{"steps":[{"accept":{}}]}}}`,
	} {
		b, err := json.Marshal(policyRowFromJSON(t, raw))
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if !strings.Contains(string(b), `"step_kinds":[]`) {
			t.Errorf("policy %s marshaled as %s, want it to contain \"step_kinds\":[]", raw, b)
		}
	}
}

// referenceStepCount recomputes step_count straight off the wire JSON,
// independent of policyListItem's struct traversal: every steps array under
// every policySteps key, exactly as the pre-step_kinds implementation did.
func referenceStepCount(t *testing.T, raw string) int {
	t.Helper()
	var doc struct {
		PolicySteps map[string]struct {
			Steps []any `json:"steps"`
		} `json:"policySteps"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("unmarshaling policy: %v", err)
	}
	n := 0
	for _, entry := range doc.PolicySteps {
		n += len(entry.Steps)
	}
	return n
}

// TestPolicyStepCountUnchangedByStepKinds is the compatibility guard.
// step_count predates step_kinds and must keep its old value on every payload
// shape — narrowing it to the baseline alongside step_kinds would silently
// change what a pre-existing field reports for any policy with conditional
// routing. The reference is computed from the raw JSON, so this fails if
// step_count is ever quietly re-pointed at the baseline.
func TestPolicyStepCountUnchangedByStepKinds(t *testing.T) {
	payloads := []string{
		autoApprovalPolicyJSON,
		appOwnerApprovalJSON,
		conditionalBranchPolicyJSON,
		`{"id":"nosteps"}`,
		`{"id":"emptymap","policyType":"POLICY_TYPE_GRANT","policySteps":{}}`,
		`{"id":"emptysteps","policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[]}}}`,
		`{"id":"nullsteps","policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":null}}}`,
		`{"id":"notype","policySteps":{"grant":{"steps":[{"accept":{}}]}}}`,
		`{"id":"branchonly","policyType":"POLICY_TYPE_GRANT","policySteps":{"0f8a-branch":{"steps":[{"accept":{}}]}}}`,
		`{"id":"manybranches","policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[{"accept":{}}]},"0a":{"steps":[{"approval":{}},{"wait":{}}]},"zz":{"steps":[{"reject":{}}]}}}`,
		`{"id":"revoke","policyType":"POLICY_TYPE_REVOKE","policySteps":{"revoke":{"steps":[{"accept":{}},{"provision":{}}]}}}`,
	}
	for _, raw := range payloads {
		want := referenceStepCount(t, raw)
		got := policyRowFromJSON(t, raw)["step_count"]
		if got != want {
			t.Errorf("policy %s: step_count = %v, want %d (the pre-step_kinds value)", raw, got, want)
		}
	}
}

// TestPolicyStepKindsUnreadableStepFailsClosed pins that a step whose kind
// can't be read reports "unknown" rather than being dropped or guessed.
// Dropping would misreport the baseline's length; guessing could make a gate
// match an auto-approval selector.
func TestPolicyStepKindsUnreadableStepFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"empty step object", `{"policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[{}]}}}`, []string{"unknown"}},
		{"two keys", `{"policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[{"accept":{},"approval":{}}]}}}`, []string{"unknown"}},
		{"not an object", `{"policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":["accept"]}}}`, []string{"unknown"}},
		{"json null step", `{"policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[null]}}}`, []string{"unknown"}},
		{"unknown alongside known", `{"policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[{},{"accept":{}}]}}}`, []string{"unknown", "accept"}},
		// All seven oneof members ship today (policy.proto), action included.
		{"every shipped kind", `{"policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[{"approval":{}},{"provision":{}},{"accept":{}},{"reject":{}},{"wait":{}},{"form":{}},{"action":{}}]}}}`,
			[]string{"approval", "provision", "accept", "reject", "wait", "form", "action"}},
		// A member added after this code was written must pass through as
		// itself, not be flattened to "unknown".
		{"future step type", `{"policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[{"quantumApproval":{}}]}}}`, []string{"quantumApproval"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := policyRowFromJSON(t, tt.raw)
			got := stepKindsOf(t, row)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("step_kinds = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestPoliciesListEmitsStepKindsEndToEnd drives the wired command against an
// httptest server, so the assertion covers the whole path (request -> decode
// -> row -> NDJSON), not just policyRow. The payloads are captured from a
// live tenant, but the server here is the test's own: this pins wiring, not
// the API's behavior.
func TestPoliciesListEmitsStepKindsEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"list":[%s,%s,%s],"nextPageToken":""}`,
			autoApprovalPolicyJSON, appOwnerApprovalJSON, conditionalBranchPolicyJSON)
	}))
	defer srv.Close()

	resetPoliciesListCmdFlags(t)
	out, err := runPoliciesListCmd(t, srv)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	rows := decodeNDJSONRows(t, out)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	// Through NDJSON, step_kinds arrives as []any of strings.
	want := map[string][]any{
		"auto":   {"accept"},
		"gate":   {"approval"},
		"branch": {"accept"},
	}
	for _, row := range rows {
		id, _ := row["id"].(string)
		got, ok := row["step_kinds"].([]any)
		if !ok {
			t.Fatalf("row %q step_kinds has type %T, want a JSON array", id, row["step_kinds"])
		}
		if !reflect.DeepEqual(got, want[id]) {
			t.Errorf("row %q step_kinds = %#v, want %#v", id, got, want[id])
		}
	}
}

// TestPoliciesSearchEmitsStepKindsEndToEnd covers the other command sharing
// policyRow. search hits a different backend than list; both were confirmed
// against a live tenant to return policySteps, which is why the same row
// builder is correct for each. The server here is again the test's own.
func TestPoliciesSearchEmitsStepKindsEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"list":[%s],"nextPageToken":""}`, autoApprovalPolicyJSON)
	}))
	defer srv.Close()

	resetPoliciesSearchCmdFlags(t)
	out, err := runPoliciesSearchCmd(t, srv)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	rows := decodeNDJSONRows(t, out)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if got, want := rows[0]["step_kinds"], []any{"accept"}; !reflect.DeepEqual(got, want) {
		t.Errorf("step_kinds = %#v, want %#v", got, want)
	}
}

// delegatingPolicyJSON is the shape a POLICY_REFERENCES_POLICY tenant returns
// for a policy that defers its baseline to another policy: baselinePolicyId
// set, policySteps empty. policy.proto makes the two mutually exclusive.
const delegatingPolicyJSON = `{"id":"deleg","displayName":"delegating baseline","description":"","systemBuiltin":false,"policyType":"POLICY_TYPE_GRANT","policySteps":{},"rules":[],"baselinePolicyId":"target"}`

// builtinDelegatingPolicyJSON is the same shape but system-builtin, so a
// selector reaches the step_kinds comparison instead of short-circuiting on
// system_builtin. Without it every delegating fixture is excluded before
// step_kinds is consulted, and a test comparing selectors over them proves
// nothing.
const builtinDelegatingPolicyJSON = `{"id":"deleg-builtin","displayName":"delegating baseline","systemBuiltin":true,"policyType":"POLICY_TYPE_GRANT","policySteps":{},"rules":[],"baselinePolicyId":"target"}`

// bothSetPolicyJSON sets baselinePolicyId AND a baseline entry. policy.proto
// makes those mutually exclusive, so a conforming server never sends it — but
// it decodes cleanly and nothing in this CLI rejects it. It is the one shape
// on which the published recipe's baseline_policy_id clause changes the
// result, which is what makes the clause defensive rather than decorative.
const bothSetPolicyJSON = `{"id":"both","policyType":"POLICY_TYPE_GRANT","systemBuiltin":true,"policySteps":{"grant":{"steps":[{"accept":{}}]}},"rules":[],"baselinePolicyId":"target"}`

// TestPolicyRowBaselinePolicyIDDistinguishesDelegationFromBrokenPolicy is why
// baseline_policy_id is on the row. A delegating policy and a policy with no
// baseline at all agree on every other key -- step_count 0, rule_count 0,
// step_kinds [] -- so without this key a healthy delegating policy reads as a
// broken one. rule_count is NOT the signal here: it is 0 unless conditional
// rules are also configured.
func TestPolicyRowBaselinePolicyIDDistinguishesDelegationFromBrokenPolicy(t *testing.T) {
	deleg := policyRowFromJSON(t, delegatingPolicyJSON)
	broken := policyRowFromJSON(t, `{"id":"broken","policyType":"POLICY_TYPE_GRANT","policySteps":{},"rules":[]}`)

	for _, key := range []string{"policy_type", "step_count", "rule_count", "step_kinds"} {
		if !reflect.DeepEqual(deleg[key], broken[key]) {
			t.Fatalf("premise broken: %s differs (%#v vs %#v); these rows are supposed to be indistinguishable without baseline_policy_id", key, deleg[key], broken[key])
		}
	}

	if got, want := deleg["baseline_policy_id"], "target"; got != want {
		t.Errorf("delegating policy baseline_policy_id = %#v, want %q", got, want)
	}
	if got := broken["baseline_policy_id"]; got != nil {
		t.Errorf("non-delegating policy baseline_policy_id = %#v (%T), want untyped nil", got, got)
	}
}

// TestPolicyRowBaselinePolicyIDIsNullNotEmptyString mirrors the deleted_at
// contract: "" is truthy in jq, so emitting it would make
// `jq 'select(.baseline_policy_id)'` match every row and silently defeat the
// key meant to surface only delegating policies.
func TestPolicyRowBaselinePolicyIDIsNullNotEmptyString(t *testing.T) {
	b, err := json.Marshal(policyRowFromJSON(t, autoApprovalPolicyJSON))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(b), `"baseline_policy_id":null`) {
		t.Errorf("marshaled row = %s, want it to contain \"baseline_policy_id\":null", b)
	}
	if strings.Contains(string(b), `"baseline_policy_id":""`) {
		t.Errorf("marshaled row = %s, baseline_policy_id rendered as empty string instead of null", b)
	}
}

// TestPolicyRowDelegatingPolicyFailsClosed pins that delegation does not make
// a policy match a kind-specific selector. The CLI cannot know the delegate's
// steps from this response, so reporting [] rather than guessing at the
// target's sequence is the safe answer; baseline_policy_id tells the caller
// where to look next.
func TestPolicyRowDelegatingPolicyFailsClosed(t *testing.T) {
	if autoApprovalSelector(t, delegatingPolicyJSON) {
		t.Error("selector matched a delegating policy whose own baseline is unknown")
	}
	row := policyRowFromJSON(t, delegatingPolicyJSON)
	if got := stepKindsOf(t, row); len(got) != 0 {
		t.Errorf("step_kinds = %#v, want empty", got)
	}
}

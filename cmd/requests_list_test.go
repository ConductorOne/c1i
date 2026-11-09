package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ConductorOne/c1i/internal/client"
)

// taskTypeKeys returns the sorted set of type keys (grant/revoke/...) present in
// a taskTypes filter, so tests can assert on type scoping without depending on
// slice order.
func taskTypeKeys(body map[string]any) []string {
	raw, ok := body["taskTypes"].([]map[string]any)
	if !ok {
		return nil
	}
	var keys []string
	for _, entry := range raw {
		for k := range entry {
			keys = append(keys, k)
		}
	}
	return keys
}

func TestBuildRequestSearchBody_ConstrainsToGrantAndRevoke(t *testing.T) {
	body := buildRequestSearchBody(requestSearchFilters{pageSize: 50})

	got := taskTypeKeys(body)
	if len(got) != 2 || !containsAll(got, "grant", "revoke") {
		t.Fatalf("default taskTypes = %v, want exactly [grant revoke]", got)
	}
	// Requests are never certify/offboarding tasks.
	if containsAll(got, "certify") {
		t.Errorf("taskTypes must not include certify: %v", got)
	}
}

func TestBuildRequestSearchBody_TypeFilter(t *testing.T) {
	for _, typ := range []string{"grant", "revoke"} {
		body := buildRequestSearchBody(requestSearchFilters{pageSize: 10, typ: typ})
		got := taskTypeKeys(body)
		if len(got) != 1 || got[0] != typ {
			t.Errorf("--type %s produced taskTypes %v, want [%s]", typ, got, typ)
		}
	}
}

func TestBuildRequestSearchBody_ScopeAndFilters(t *testing.T) {
	state, err := taskStateFlag.parse("open")
	if err != nil {
		t.Fatal(err)
	}
	body := buildRequestSearchBody(requestSearchFilters{
		pageSize:      25,
		pageToken:     "tok",
		scopeUserID:   "user-1",
		appID:         "app-1",
		entitlementID: "ent-1",
		state:         state,
	})

	if body["openerOrSubjectUserId"] != "user-1" {
		t.Errorf("openerOrSubjectUserId = %v, want user-1", body["openerOrSubjectUserId"])
	}
	if body["pageToken"] != "tok" {
		t.Errorf("pageToken = %v, want tok", body["pageToken"])
	}
	if !reflect.DeepEqual(body["applicationIds"], []string{"app-1"}) {
		t.Errorf("applicationIds = %v, want [app-1]", body["applicationIds"])
	}
	if !reflect.DeepEqual(body["appEntitlementIds"], []string{"ent-1"}) {
		t.Errorf("appEntitlementIds = %v, want [ent-1]", body["appEntitlementIds"])
	}
	// --state open must be mapped to the API enum, not passed through raw.
	if !reflect.DeepEqual(body["taskStates"], []string{"TASK_STATE_OPEN"}) {
		t.Errorf("taskStates = %v, want [TASK_STATE_OPEN]", body["taskStates"])
	}
}

func TestBuildRequestSearchBody_TenantWideOmitsScope(t *testing.T) {
	body := buildRequestSearchBody(requestSearchFilters{pageSize: 50}) // scopeUserID empty (--all)
	if _, present := body["openerOrSubjectUserId"]; present {
		t.Errorf("openerOrSubjectUserId should be omitted for a tenant-wide (--all) query")
	}
	if _, present := body["pageToken"]; present {
		t.Errorf("pageToken should be omitted when empty")
	}
}

func TestTaskRow(t *testing.T) {
	grant := taskSummary{ID: "t1", State: "TASK_STATE_OPEN"}
	grant.Type.Grant = &taskGrantRevoke{AppID: "app", AppEntitlementID: "ent", Outcome: "TASK_TYPE_ACTION_OUTCOME_APPROVED"}
	row := taskRow(grant)
	if row["type"] != "grant" || row["app_id"] != "app" || row["app_entitlement_id"] != "ent" {
		t.Errorf("grant row = %v", row)
	}
	if row["outcome"] != "TASK_TYPE_ACTION_OUTCOME_APPROVED" {
		t.Errorf("grant outcome = %q, want the terminal outcome", row["outcome"])
	}

	// An *_OUTCOME_UNSPECIFIED value is proto-default noise and must be dropped.
	revoke := taskSummary{ID: "t2"}
	revoke.Type.Revoke = &taskGrantRevoke{AppID: "app2", Outcome: "TASK_TYPE_ACTION_OUTCOME_UNSPECIFIED"}
	row = taskRow(revoke)
	if row["type"] != "revoke" {
		t.Errorf("revoke row type = %q", row["type"])
	}
	if _, present := row["outcome"]; present {
		t.Errorf("unspecified outcome should be omitted, got %q", row["outcome"])
	}
}

func containsAll(haystack []string, needles ...string) bool {
	set := make(map[string]bool, len(haystack))
	for _, h := range haystack {
		set[h] = true
	}
	for _, n := range needles {
		if !set[n] {
			return false
		}
	}
	return true
}

// An entitlement id repeats across apps, so --entitlement-id without --app-id
// would merge requests from every app sharing the id.
func TestRequestsListEntitlementIDRequiresAppID(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"list":[]}`))
	}))
	defer srv.Close()
	resetCmdFlags(t, requestsListCmd)
	_ = requestsListCmd.Flags().Set("all", "true")
	_ = requestsListCmd.Flags().Set("entitlement-id", "ent-1")
	requestsListCmd.SetContext(context.Background())
	t.Setenv("C1I_URL", "https://example.invalid")
	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
	err := requestsListCmd.RunE(requestsListCmd, nil)
	if exitCode(err) != exitUsage {
		t.Fatalf("err = %v (exit %d), want a usage error (exit 2)", err, exitCode(err))
	}
	if requests != 0 {
		t.Errorf("sent %d requests, want none", requests)
	}
}

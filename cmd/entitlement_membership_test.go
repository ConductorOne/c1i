package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// runMembershipCLI runs args with both the list and the mutation client
// pointed at srv.
func runMembershipCLI(t *testing.T, srv *mutServer, args ...string) mutResult {
	t.Helper()
	t.Setenv("C1I_URL", mutTestURL)
	return runCLI(t, func() {
		stubMutationClient(t, srv.srv, 0)
		orig := newListClient
		newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
			return client.NewForTesting(srv.srv.URL, srv.srv.Client(), client.WithMaxRetries(0)), nil
		}
		t.Cleanup(func() { newListClient = orig })
	}, nil, args...)
}

func ndjsonRows(t *testing.T, out string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("row %q: %v", line, err)
		}
		rows = append(rows, row)
	}
	return rows
}

func TestEntitlementsGrantsPagesAndKeepsTypes(t *testing.T) {
	page1 := `{"list":[{"appId":"a1","appEntitlementId":"e1","appUserId":"au1","appUser":{"id":"au1","displayName":"zz one","identityUserId":"u1"},"user":{"displayName":"zz user","email":"zz@example.com"},"discovered":"2026-11-18T00:00:00Z","expired":"2026-11-20T00:00:00Z","grantReasons":[],"grantSources":[{"appId":"a2","id":"g1"}]}],"nextPageToken":"t2"}`
	page2 := `{"list":[{"appId":"a1","appEntitlementId":"e1","appUserId":"au2","appUser":{"id":"au2"},"user":null,"discovered":null,"expired":null,"grantReasons":null,"grantSources":[]}],"nextPageToken":""}`
	srv := newMutServer(t, func(r *http.Request, n int) mutReply {
		if r.URL.Query().Get("page_token") == "t2" {
			return mutReply{body: page2}
		}
		return mutReply{body: page1}
	})
	r := runMembershipCLI(t, srv, "entitlements", "grants", "e1", "--app-id", "a1")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	rows := ndjsonRows(t, r.stdout)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2: %s", len(rows), r.stdout)
	}
	if rows[0]["expires_at"] != "2026-11-20T00:00:00Z" || rows[0]["discovered_at"] != "2026-11-18T00:00:00Z" {
		t.Errorf("row 0 times = %v / %v", rows[0]["expires_at"], rows[0]["discovered_at"])
	}
	if rows[0]["grant_source_count"] != float64(1) || rows[0]["user_email"] != "zz@example.com" || rows[0]["identity_user_id"] != "u1" {
		t.Errorf("row 0 = %v", rows[0])
	}
	if v, ok := rows[1]["expires_at"]; !ok || v != nil {
		t.Errorf("row 1 expires_at = %v (present %v), want null", v, ok)
	}
	if rows[1]["grant_reason_count"] != float64(0) {
		t.Errorf("row 1 grant_reason_count = %v, want 0", rows[1]["grant_reason_count"])
	}
	sent := srv.requests()
	if len(sent) != 2 || sent[0].rawPath != "/api/v1/apps/a1/entitlements/e1/grants" {
		t.Errorf("requests = %+v", sent)
	}
}

func TestGrantsAccountsReadsBindings(t *testing.T) {
	srv := newMutServer(t, fixedReply(200, `{"bindings":[{"appId":"a1","appEntitlementId":"e1","appUserId":"au1","createdAt":"2026-11-18T00:00:00Z","deletedAt":null,"deprovisionAt":null,"grantSources":[]}]}`))
	r := runMembershipCLI(t, srv, "grants", "accounts", "--app-id", "a1", "--entitlement-id", "e1", "--user-id", "u1")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	rows := ndjsonRows(t, r.stdout)
	if len(rows) != 1 || rows[0]["app_user_id"] != "au1" || rows[0]["deprovision_at"] != nil || rows[0]["grant_source_count"] != float64(0) {
		t.Fatalf("rows = %v", rows)
	}
	if got := srv.requests()[0].rawPath; got != "/api/v1/apps/a1/entitlements/e1/users/u1/grants" {
		t.Errorf("path = %s", got)
	}
}

func TestGrantsUpdateDurationRefusesANonRFC3339Time(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	r := runMutationCLI(t, nil, 0, nil, "grants", "update-duration", "--app-id", "a1", "--entitlement-id", "e1", "--app-user-id", "au1", "--deprovision-at", "tomorrow")
	if r.exit != exitUsage || !strings.Contains(r.err.Error(), "RFC3339") {
		t.Fatalf("exit %d (%v), want 2 naming RFC3339", r.exit, r.err)
	}
}

func TestGrantsUpdateDurationSendsTheTimestamp(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := newMutServer(t, fixedReply(200, `{"binding":{"appUserId":"au1","deprovisionAt":"2026-12-01T00:00:00Z"}}`))
	r := runMutationCLI(t, srv.srv, 0, nil, "grants", "update-duration", "--app-id", "a1", "--entitlement-id", "e1", "--app-user-id", "au1", "--deprovision-at", "2026-12-01T00:00:00Z")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	sent := srv.requests()
	if len(sent) != 1 || sent[0].body != `{"newDeprovisionAt":"2026-12-01T00:00:00Z"}` {
		t.Errorf("requests = %+v", sent)
	}
}

func TestAutomationConditionFlagsAreExclusive(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, extra := range [][]string{
		{"--cel", "true", "--none"},
		{"--refs-file", "x.json", "--none"},
		{"--none=false"},
	} {
		args := append([]string{"entitlements", "automation", "create", "e1", "--app-id", "a1", "--display-name", "zz-a"}, extra...)
		r := runMutationCLI(t, nil, 0, nil, args...)
		if r.exit != exitUsage || !strings.Contains(r.err.Error(), "--none") {
			t.Errorf("%v: exit %d (%v), want 2 naming the conflict", extra, r.exit, r.err)
		}
	}
}

func TestAutomationCreateBodyFileExcludesFlags(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	file := writeTempFile(t, `{"displayName":"zz-a","none":{}}`)
	r := runMutationCLI(t, nil, 0, nil, "entitlements", "automation", "create", "e1", "--app-id", "a1", "--body-file", file, "--cel", "true")
	if r.exit != exitUsage || !strings.Contains(r.err.Error(), "mutually exclusive with --cel") {
		t.Fatalf("exit %d (%v), want 2 naming --cel", r.exit, r.err)
	}
}

func TestAutomationCreateWireShapes(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	refs := writeTempFile(t, `[{"appId":"a2","id":"e2"}]`)
	for _, tc := range []struct {
		extra []string
		want  string
	}{
		{[]string{"--cel", `subject.email == "x"`}, `{"automation":{"cel":{"expression":"subject.email == \"x\""},"displayName":"zz-a"}}`},
		{[]string{"--none"}, `{"automation":{"displayName":"zz-a","none":{}}}`},
		{[]string{"--refs-file", refs}, `{"automation":{"displayName":"zz-a","entitlements":{"entitlementRefs":[{"appId":"a2","id":"e2"}]}}}`},
	} {
		srv := newMutServer(t, fixedReply(200, `{"automation":{"appEntitlementId":"e1"}}`))
		args := append([]string{"entitlements", "automation", "create", "e1", "--app-id", "a1", "--display-name", "zz-a"}, tc.extra...)
		r := runMutationCLI(t, srv.srv, 0, nil, args...)
		if r.err != nil {
			t.Fatalf("%v: exit %d: %v", tc.extra, r.exit, r.err)
		}
		sent := srv.requests()
		write := sent[len(sent)-1]
		if write.rawPath != "/api/v1/apps/a1/entitlements/e1/automation/create" || compactRaw(t, write.body) != tc.want {
			t.Errorf("%v: write = %s %s, want body %s", tc.extra, write.rawPath, write.body, tc.want)
		}
	}
}

// The server stores an unknown entitlement reference, so c1i looks each up.
func TestAutomationUnknownRefExitsNotFoundBeforeTheWrite(t *testing.T) {
	refs := writeTempFile(t, `[{"appId":"a2","id":"e-missing"}]`)
	for _, dry := range []bool{false, true} {
		t.Setenv("C1I_URL", mutTestURL)
		if dry {
			withDryRun(t)
		}
		srv := newMutServer(t, func(r *http.Request, _ int) mutReply {
			if r.Method == http.MethodGet {
				return mutReply{status: 404, body: `{"code":5,"message":"not found"}`}
			}
			return mutReply{body: `{"automation":{}}`}
		})
		r := runMutationCLI(t, srv.srv, 0, nil, "entitlements", "automation", "create", "e1", "--app-id", "a1", "--display-name", "zz-a", "--refs-file", refs)
		if r.exit != exitNotFound {
			t.Errorf("dry=%v: exit %d (%v), want 4", dry, r.exit, r.err)
		}
		for _, q := range srv.requests() {
			if q.method != http.MethodGet {
				t.Errorf("dry=%v: sent %s %s", dry, q.method, q.rawPath)
			}
		}
	}
}

func TestAutomationUpdateMasksTheConditionAsConditions(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := newMutServer(t, fixedReply(200, `{"AppEntitlementAutomation":{"appEntitlementId":"e1"}}`))
	r := runMutationCLI(t, srv.srv, 0, nil, "entitlements", "automation", "update", "e1", "--app-id", "a1", "--display-name", "zz-b", "--cel", "true")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	sent := srv.requests()
	if len(sent) != 1 {
		t.Fatalf("requests = %+v, want only the write", sent)
	}
	if want := `{"cel":{"expression":"true"},"displayName":"zz-b","updateMask":"displayName,conditions"}`; compactRaw(t, sent[0].body) != want {
		t.Errorf("body = %s, want %s", sent[0].body, want)
	}
}

func TestAutomationUpdateSendsTheCurrentNameWhenNoneIsGiven(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := newMutServer(t, func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			return mutReply{body: `{"AppEntitlementAutomation":{"appEntitlementId":"e1","displayName":"zz-stored"}}`}
		}
		return mutReply{body: `{"AppEntitlementAutomation":{"appEntitlementId":"e1"}}`}
	})
	r := runMutationCLI(t, srv.srv, 0, nil, "entitlements", "automation", "update", "e1", "--app-id", "a1", "--none")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	sent := srv.requests()
	if len(sent) != 2 || sent[0].method != http.MethodGet || sent[0].rawPath != "/api/v1/apps/a1/entitlements/e1/automation" {
		t.Fatalf("requests = %+v", sent)
	}
	if want := `{"displayName":"zz-stored","none":{},"updateMask":"conditions"}`; compactRaw(t, sent[1].body) != want {
		t.Errorf("body = %s, want %s", sent[1].body, want)
	}
}

func TestAutomationUpdateWithoutAnAutomationExitsNotFound(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := newMutServer(t, fixedReply(404, `{"code":5,"message":"not found"}`))
	r := runMutationCLI(t, srv.srv, 0, nil, "entitlements", "automation", "update", "e1", "--app-id", "a1", "--none")
	if r.exit != exitNotFound {
		t.Fatalf("exit %d (%v), want 4", r.exit, r.err)
	}
	if n := len(srv.requests()); n != 1 {
		t.Errorf("requests = %d, want the read only", n)
	}
}

func TestAutomationExclusionsListRows(t *testing.T) {
	srv := newMutServer(t, fixedReply(200, `{"list":[{"appId":"a1","appEntitlementId":"other","appUserId":"u1","user":{"id":"u1","displayName":"zz","email":"zz@example.com","status":"ENABLED"}}],"nextPageToken":""}`))
	r := runMembershipCLI(t, srv, "entitlements", "automation", "exclusions", "list", "e1", "--app-id", "a1")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	rows := ndjsonRows(t, r.stdout)
	want := map[string]any{"user_id": "u1", "display_name": "zz", "email": "zz@example.com", "status": "ENABLED"}
	if len(rows) != 1 || len(rows[0]) != len(want) {
		t.Fatalf("rows = %v", rows)
	}
	for k, v := range want {
		if rows[0][k] != v {
			t.Errorf("%s = %v, want %v", k, rows[0][k], v)
		}
	}
}

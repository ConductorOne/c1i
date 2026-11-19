package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// acctProvLiveConn/acctProvDeletedConn are GET .../connectors/cn1 bodies.
const (
	acctProvLiveConn    = `{"connectorView":{"connector":{"id":"cn1","appId":"a1"}}}`
	acctProvDeletedConn = `{"connectorView":{"connector":{"id":"cn1","appId":"a1","deletedAt":"2026-01-01T00:00:00Z"}}}`
	acctProvNotFound    = `{"code":5,"message":"not found"}`
	acctProvWriteBody   = `{"appEntitlementView":{"appEntitlement":{"id":"e-1","provisionerPolicy":{"connector":{"account":{"connectorId":"cn1"}}}}}}`
)

// acctProvRoute answers every connector GET with connStatus/connBody, every
// CEL-test POST with the next of testReplies (repeating the last, or a
// canned success when testReplies is empty), and anything else (the
// entitlement write) with writeBody.
func acctProvRoute(connStatus int, connBody string, testReplies []mutReply, writeBody string) func(*http.Request, int) mutReply {
	testN := 0
	return func(r *http.Request, _ int) mutReply {
		switch {
		case r.Method == http.MethodGet:
			return mutReply{status: connStatus, body: connBody}
		case strings.HasSuffix(r.URL.Path, "/test-account-provision-policy"):
			if len(testReplies) == 0 {
				return mutReply{body: `{"value":"x","type":"string"}`}
			}
			i := testN
			if i >= len(testReplies) {
				i = len(testReplies) - 1
			}
			testN++
			return testReplies[i]
		default:
			return mutReply{body: writeBody}
		}
	}
}

// TestEntitlementsAccountProvisioningShow pins "show"'s output to exactly
// .provisionerPolicy, "{}" when the entitlement has none, and the plain
// entitlement-read exit codes.
func TestEntitlementsAccountProvisioningShow(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		get      string
		wantExit int
		want     string
	}{
		{
			name:     "configured",
			status:   200,
			get:      `{"appEntitlementView":{"appEntitlement":{"id":"e-1","provisionerPolicy":{"connector":{"account":{"connectorId":"cn1"}}}}}}`,
			wantExit: 0,
			want:     indentJSON(t, `{"connector":{"account":{"connectorId":"cn1"}}}`),
		},
		{
			name:     "unconfigured",
			status:   200,
			get:      `{"appEntitlementView":{"appEntitlement":{"id":"e-1"}}}`,
			wantExit: 0,
			want:     "{}\n",
		},
		{name: "unknown entitlement", status: 404, get: acctProvNotFound, wantExit: exitNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, fixedReply(tc.status, tc.get))
			r := runMutationCLI(t, s.srv, 0, nil, "entitlements", "account-provisioning", "show", "e-1", "--app-id", "a1")
			if r.exit != tc.wantExit {
				t.Fatalf("exit %d (err %v), want %d\n%s", r.exit, r.err, tc.wantExit, r.stderr)
			}
			if tc.wantExit == 0 && r.stdout != tc.want {
				t.Errorf("stdout = %q, want %q", r.stdout, tc.want)
			}
		})
	}
}

// TestEntitlementsAccountProvisioningSetBody pins the exact provisionerPolicy
// sent for each storage mode: a fixed schemaId "", the --field map, and
// either saveToVault or doNotSave -- never both, never neither.
func TestEntitlementsAccountProvisioningSetBody(t *testing.T) {
	for _, tc := range []struct {
		name     string
		extra    []string
		wantArm  string
		wantArmV any
	}{
		{name: "save to vault", extra: []string{"--save-to-vault-id", "v1", "--save-to-vault-id", "v2"},
			wantArm: "saveToVault", wantArmV: map[string]any{"vaultIds": []any{"v1", "v2"}}},
		{name: "do not save", extra: []string{"--do-not-save"},
			wantArm: "doNotSave", wantArmV: map[string]any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, acctProvRoute(200, acctProvLiveConn, nil, acctProvWriteBody))
			args := append([]string{"entitlements", "account-provisioning", "set", "e-1", "--app-id", "a1", "--connector-id", "cn1",
				"--field", "email=subject.email", "--field", "given_name=subject.display_name"}, tc.extra...)
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != 0 {
				t.Fatalf("exit %d (err %v)\n%s", r.exit, r.err, r.stderr)
			}
			reqs := s.requests()
			write := reqs[len(reqs)-1]
			if write.method != http.MethodPost || write.rawPath != "/api/v1/apps/a1/entitlements/e-1" {
				t.Fatalf("write = %s %s, want POST /api/v1/apps/a1/entitlements/e-1", write.method, write.rawPath)
			}
			var body struct {
				Entitlement struct {
					ID                string         `json:"id"`
					AppID             string         `json:"appId"`
					ProvisionerPolicy map[string]any `json:"provisionerPolicy"`
				} `json:"entitlement"`
				UpdateMask string `json:"updateMask"`
			}
			if err := json.Unmarshal([]byte(write.body), &body); err != nil {
				t.Fatalf("write body is not JSON: %v (%s)", err, write.body)
			}
			if body.Entitlement.ID != "e-1" || body.Entitlement.AppID != "a1" || body.UpdateMask != "provisionerPolicy" {
				t.Errorf("entitlement/mask = %+v %q, want id=e-1 appId=a1 mask=provisionerPolicy", body.Entitlement, body.UpdateMask)
			}
			account, _ := body.Entitlement.ProvisionerPolicy["connector"].(map[string]any)["account"].(map[string]any)
			if account["connectorId"] != "cn1" || account["schemaId"] != "" {
				t.Errorf("account = %+v, want connectorId=cn1 schemaId=\"\"", account)
			}
			wantConfig := map[string]any{"email": "subject.email", "given_name": "subject.display_name"}
			if got := account["config"]; !mapsEqual(got, wantConfig) {
				t.Errorf("config = %v, want %v", got, wantConfig)
			}
			if got := account[tc.wantArm]; !mapsEqual(got, tc.wantArmV) {
				t.Errorf("%s = %v, want %v", tc.wantArm, got, tc.wantArmV)
			}
			for _, other := range []string{"saveToVault", "doNotSave"} {
				if other != tc.wantArm {
					if _, present := account[other]; present {
						t.Errorf("account carries unwanted %q arm: %v", other, account)
					}
				}
			}
		})
	}
}

// mapsEqual compares two decoded JSON values by their marshaled form, so a
// map[string]any and its literal compare equal regardless of key order.
func mapsEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

// TestEntitlementsAccountProvisioningSetUsageErrors pins every flag-shape
// refusal as exit 2 with no request sent at all.
func TestEntitlementsAccountProvisioningSetUsageErrors(t *testing.T) {
	base := []string{"entitlements", "account-provisioning", "set", "e-1", "--app-id", "a1", "--connector-id", "cn1"}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "no --field", args: append(append([]string{}, base...), "--do-not-save")},
		{name: "malformed --field", args: append(append([]string{}, base...), "--field", "novalue", "--do-not-save")},
		{name: "duplicate --field key", args: append(append([]string{}, base...), "--field", "email=a", "--field", "email=b", "--do-not-save")},
		{name: "neither storage mode", args: append(append([]string{}, base...), "--field", "email=a")},
		{name: "both storage modes", args: append(append([]string{}, base...), "--field", "email=a", "--save-to-vault-id", "v1", "--do-not-save")},
		{name: "--do-not-save=false", args: append(append([]string{}, base...), "--field", "email=a", "--do-not-save=false")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, acctProvRoute(200, acctProvLiveConn, nil, acctProvWriteBody))
			r := runMutationCLI(t, s.srv, 0, nil, tc.args...)
			if r.exit != exitUsage {
				t.Fatalf("exit %d (err %v), want %d\n%s", r.exit, r.err, exitUsage, r.stderr)
			}
			if len(s.requests()) != 0 {
				t.Errorf("requests sent: %v", s.requests())
			}
		})
	}
}

// TestEntitlementsAccountProvisioningSetConnectorPreRead pins that
// --connector-id is read before the write, --dry-run included: unknown
// exits 4, deleted exits 2, and no write is ever sent in either case.
func TestEntitlementsAccountProvisioningSetConnectorPreRead(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		get       string
		dry       bool
		wantExit  int
		wantWrite bool
	}{
		{name: "live", status: 200, get: acctProvLiveConn, wantExit: 0, wantWrite: true},
		{name: "live dry-run", status: 200, get: acctProvLiveConn, dry: true, wantExit: 0, wantWrite: false},
		{name: "unknown", status: 404, get: acctProvNotFound, wantExit: exitNotFound},
		{name: "unknown dry-run", status: 404, get: acctProvNotFound, dry: true, wantExit: exitNotFound},
		{name: "deleted", status: 200, get: acctProvDeletedConn, wantExit: exitUsage},
		{name: "deleted dry-run", status: 200, get: acctProvDeletedConn, dry: true, wantExit: exitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, acctProvRoute(tc.status, tc.get, nil, acctProvWriteBody))
			args := []string{"entitlements", "account-provisioning", "set", "e-1", "--app-id", "a1", "--connector-id", "cn1",
				"--field", "email=subject.email", "--do-not-save"}
			if tc.dry {
				args = append(args, "--dry-run")
			}
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != tc.wantExit {
				t.Fatalf("exit %d (err %v), want %d\n%s", r.exit, r.err, tc.wantExit, r.stderr)
			}
			reqs := s.requests()
			if len(reqs) == 0 || reqs[0].method != http.MethodGet || reqs[0].rawPath != "/api/v1/apps/a1/connectors/cn1" {
				t.Errorf("first request %v, want the connector GET", reqs)
			}
			if got := len(writesIn(reqs)) > 0; got != tc.wantWrite {
				t.Errorf("write sent = %v, want %v (%v)", got, tc.wantWrite, reqs)
			}
		})
	}
}

// TestEntitlementsAccountProvisioningSetTest pins --test: every --field is
// evaluated in order before the write (--dry-run too), and the first one the
// server rejects aborts with no write and names its key.
func TestEntitlementsAccountProvisioningSetTest(t *testing.T) {
	badCEL := mutReply{status: 500, body: `{"code":13,"message":"ERROR: <input>:1:1: Syntax error"}`}
	ok := mutReply{body: `{"value":"x","type":"string"}`}

	for _, tc := range []struct {
		name        string
		testReplies []mutReply
		dry         bool
		wantExit    int
		wantErr     string
		wantWrite   bool
	}{
		{name: "both compile", testReplies: []mutReply{ok, ok}, wantExit: 0, wantWrite: true},
		{name: "second field rejected", testReplies: []mutReply{ok, badCEL}, wantExit: exitServer, wantErr: "given_name"},
		{name: "second field rejected dry-run", testReplies: []mutReply{ok, badCEL}, dry: true, wantExit: exitServer, wantErr: "given_name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, acctProvRoute(200, acctProvLiveConn, tc.testReplies, acctProvWriteBody))
			args := []string{"entitlements", "account-provisioning", "set", "e-1", "--app-id", "a1", "--connector-id", "cn1",
				"--field", "email=subject.email", "--field", "given_name=subject.display_name", "--do-not-save", "--test"}
			if tc.dry {
				args = append(args, "--dry-run")
			}
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != tc.wantExit {
				t.Fatalf("exit %d (err %v), want %d\n%s", r.exit, r.err, tc.wantExit, r.stderr)
			}
			if tc.wantErr != "" && (r.err == nil || !strings.Contains(r.err.Error(), tc.wantErr)) {
				t.Errorf("err %v, want it to mention %q", r.err, tc.wantErr)
			}
			reqs := s.requests()
			entWrite := false
			for _, req := range reqs {
				if req.method == http.MethodPost && strings.Contains(req.rawPath, "/entitlements/") {
					entWrite = true
				}
			}
			if entWrite != tc.wantWrite {
				t.Errorf("entitlement write sent = %v, want %v (%v)", entWrite, tc.wantWrite, reqs)
			}
			tests := 0
			for _, req := range reqs {
				if strings.HasSuffix(req.rawPath, "/test-account-provision-policy") {
					tests++
				}
			}
			if tests != len(tc.testReplies) {
				t.Errorf("ran %d CEL tests, want %d (%v)", tests, len(tc.testReplies), reqs)
			}
		})
	}
}

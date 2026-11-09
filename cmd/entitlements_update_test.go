package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// runEntitlementsUpdate drives the real command against srv with only the given
// flags set, returning stdout.
func runEntitlementsUpdate(t *testing.T, srv *httptest.Server, dryRun bool, id string, flags map[string]string) (string, error) {
	t.Helper()
	resetCmdFlags(t, entitlementsUpdateCmd)
	stubNewClient(t, srv)
	orig := viper.GetBool("dry_run")
	viper.Set("dry_run", dryRun)
	t.Cleanup(func() { viper.Set("dry_run", orig) })
	t.Setenv("C1I_URL", "https://example.invalid")
	for k, v := range flags {
		if err := entitlementsUpdateCmd.Flags().Set(k, v); err != nil {
			t.Fatalf("setting --%s: %v", k, err)
		}
	}
	var out bytes.Buffer
	entitlementsUpdateCmd.SetOut(&out)
	entitlementsUpdateCmd.SetContext(context.Background())
	err := entitlementsUpdateCmd.RunE(entitlementsUpdateCmd, []string{id})
	return out.String(), err
}

// entitlementTestPolicies are the policies captureServer serves; the emergency
// slot takes a grant policy.
var entitlementTestPolicies = map[string]string{
	"pol-g": "POLICY_TYPE_GRANT", "pol-r": "POLICY_TYPE_REVOKE",
	"pol-c": "POLICY_TYPE_CERTIFY", "pol-e": "POLICY_TYPE_GRANT",
}

// captureServer answers policy lookups from entitlementTestPolicies and
// records the one mutation it gets, answering with an entitlement envelope.
func captureServer(t *testing.T, gotPath *string, gotBody *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if id, ok := strings.CutPrefix(r.URL.Path, "/api/v1/policies/"); ok && r.Method == http.MethodGet {
			_, _ = fmt.Fprintf(w, `{"policy":{"id":%q,"policyType":%q,"deletedAt":null}}`, id, entitlementTestPolicies[id])
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		*gotPath = r.URL.EscapedPath()
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, gotBody); err != nil {
			t.Errorf("unmarshal body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"appEntitlementView":{"appEntitlement":{"id":"ent-1"}}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestEntitlementsUpdateBodyPerFlag pins, per flag, the exact entitlement
// fields and update mask sent: one mask path per flag passed, nothing else.
func TestEntitlementsUpdateBodyPerFlag(t *testing.T) {
	cases := []struct {
		name   string
		flags  map[string]string
		fields map[string]any
		mask   string
	}{
		{"display name", map[string]string{"display-name": "New"}, map[string]any{"displayName": "New"}, "displayName"},
		{"description", map[string]string{"description": "d"}, map[string]any{"description": "d"}, "description"},
		{"description cleared", map[string]string{"description": ""}, map[string]any{"description": ""}, "description"},
		{"grant policy", map[string]string{"grant-policy-id": "pol-g"}, map[string]any{"grantPolicyId": "pol-g"}, "grantPolicyId"},
		{"revoke policy", map[string]string{"revoke-policy-id": "pol-r"}, map[string]any{"revokePolicyId": "pol-r"}, "revokePolicyId"},
		{"certify policy", map[string]string{"certify-policy-id": "pol-c"}, map[string]any{"certifyPolicyId": "pol-c"}, "certifyPolicyId"},
		{"emergency policy", map[string]string{"emergency-grant-policy-id": "pol-e"}, map[string]any{"emergencyGrantPolicyId": "pol-e"}, "emergencyGrantPolicyId"},
		{"grant policy unbound", map[string]string{"unbind-policy": "grant"}, map[string]any{"grantPolicyId": ""}, "grantPolicyId"},
		{"revoke policy unbound", map[string]string{"unbind-policy": "revoke"}, map[string]any{"revokePolicyId": ""}, "revokePolicyId"},
		{"certify policy unbound", map[string]string{"unbind-policy": "certify"}, map[string]any{"certifyPolicyId": ""}, "certifyPolicyId"},
		{"emergency policy unbound", map[string]string{"unbind-policy": "emergency-grant"}, map[string]any{"emergencyGrantPolicyId": ""}, "emergencyGrantPolicyId"},
		{
			"emergency disabled and unbound",
			map[string]string{"emergency-grant-enabled": "false", "unbind-policy": "emergency-grant"},
			map[string]any{"emergencyGrantEnabled": false, "emergencyGrantPolicyId": ""},
			"emergencyGrantPolicyId,emergencyGrantEnabled",
		},
		{"emergency disabled", map[string]string{"emergency-grant-enabled": "false"}, map[string]any{"emergencyGrantEnabled": false}, "emergencyGrantEnabled"},
		{
			"emergency enabled with policy",
			map[string]string{"emergency-grant-enabled": "true", "emergency-grant-policy-id": "pol-e"},
			map[string]any{"emergencyGrantEnabled": true, "emergencyGrantPolicyId": "pol-e"},
			"emergencyGrantPolicyId,emergencyGrantEnabled",
		},
		{"duration go form", map[string]string{"duration-grant": "1h30m"}, map[string]any{"durationGrant": "5400s"}, "durationGrant"},
		{"duration whitespace trimmed", map[string]string{"duration-grant": " 8h "}, map[string]any{"durationGrant": "28800s"}, "durationGrant"},
		{"duration zero is sent", map[string]string{"duration-grant": "0s"}, map[string]any{"durationGrant": "0s"}, "durationGrant"},
		{"duration seconds form", map[string]string{"duration-grant": "3600s"}, map[string]any{"durationGrant": "3600s"}, "durationGrant"},
		{"duration sub-second", map[string]string{"duration-grant": "1s500ms"}, map[string]any{"durationGrant": "1.5s"}, "durationGrant"},
		{"duration max keeps every nanosecond", map[string]string{"duration-grant": "2562047h47m16s854775807ns"}, map[string]any{"durationGrant": "9223372036.854775807s"}, "durationGrant"},
		{"duration one nanosecond", map[string]string{"duration-grant": "1ns"}, map[string]any{"durationGrant": "0.000000001s"}, "durationGrant"},
		{"duration unset", map[string]string{"duration-unset": "true"}, map[string]any{"durationUnset": map[string]any{}}, "durationUnset"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var path string
			var body map[string]any
			srv := captureServer(t, &path, &body)
			if _, err := runEntitlementsUpdate(t, srv, false, "ent-1", withAppID(tc.flags)); err != nil {
				t.Fatalf("RunE: %v", err)
			}
			want := map[string]any{"id": "ent-1", "appId": "app-1"}
			for k, v := range tc.fields {
				want[k] = v
			}
			if !reflect.DeepEqual(body["entitlement"], want) {
				t.Errorf("entitlement = %#v, want %#v", body["entitlement"], want)
			}
			if body["updateMask"] != tc.mask {
				t.Errorf("updateMask = %v, want %q", body["updateMask"], tc.mask)
			}
			if len(body) != 2 {
				t.Errorf("top-level keys = %v, want only entitlement and updateMask", body)
			}
		})
	}
}

func withAppID(flags map[string]string) map[string]string {
	out := map[string]string{"app-id": "app-1"}
	for k, v := range flags {
		out[k] = v
	}
	return out
}

// TestEntitlementsUpdateUsageErrors pins every refusal as exit 2 before any
// request is sent.
func TestEntitlementsUpdateUsageErrors(t *testing.T) {
	cases := []struct {
		name    string
		flags   map[string]string
		wantMsg string
	}{
		{"nothing to update", map[string]string{}, "nothing to update"},
		{"empty display name", map[string]string{"display-name": ""}, "--display-name requires a non-empty value"},
		{"whitespace display name", map[string]string{"display-name": " \t "}, "--display-name requires a non-empty value"},
		{"duration grant and unset", map[string]string{"duration-grant": "1h", "duration-unset": "true"}, "mutually exclusive"},
		{"duration unset false", map[string]string{"duration-unset": "false"}, "--duration-unset"},
		{"duration unparseable", map[string]string{"duration-grant": "soon"}, "--duration-grant"},
		{"duration negative", map[string]string{"duration-grant": "-1m"}, "whole-number duration"},
		{"duration decimal", map[string]string{"duration-grant": "1.5h"}, "whole-number duration"},
		{"emergency enabled without policy", map[string]string{"emergency-grant-enabled": "true"}, "emergency grant policy id must be set if emergency grant is enabled"},
		{"emergency enabled with policy unbound", map[string]string{"emergency-grant-enabled": "true", "unbind-policy": "emergency-grant"}, "emergency grant policy id must be set if emergency grant is enabled"},
		{"unknown unbind kind", map[string]string{"unbind-policy": "emergency"}, `invalid --unbind-policy "emergency"`},
		{"set and unbind emergency", map[string]string{"emergency-grant-policy-id": "pol-e", "unbind-policy": "emergency-grant"}, "--emergency-grant-policy-id and --unbind-policy emergency-grant"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("no request expected, got %s %s", r.Method, r.URL.Path)
			}))
			t.Cleanup(srv.Close)
			_, err := runEntitlementsUpdate(t, srv, false, "ent-1", withAppID(tc.flags))
			var ue *usageError
			if !errors.As(err, &ue) {
				t.Fatalf("err = %v, want *usageError", err)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("err = %q, want it to contain %q", err, tc.wantMsg)
			}
		})
	}
}

// TestEntitlementsUpdateEscapesBothIDs pins that the app id and entitlement id
// are each escaped as one path segment.
func TestEntitlementsUpdateEscapesBothIDs(t *testing.T) {
	var path string
	var body map[string]any
	srv := captureServer(t, &path, &body)
	if _, err := runEntitlementsUpdate(t, srv, false, "ent/1 ?#", map[string]string{"app-id": "app/1 ?", "description": "d"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if want := "/api/v1/apps/app%2F1%20%3F/entitlements/ent%2F1%20%3F%23"; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

// TestEntitlementsUpdateDryRunSendsNothing pins that --dry-run previews the
// wrapped body and never reaches the server.
func TestEntitlementsUpdateDryRunSendsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("dry-run must not send, got %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	out, err := runEntitlementsUpdate(t, srv, true, "ent-1", withAppID(map[string]string{"grant-policy-id": "pol-g"}))
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	for _, want := range []string{`"url": "https://example.invalid/api/v1/apps/app-1/entitlements/ent-1"`, `"grantPolicyId": "pol-g"`, `"updateMask": "grantPolicyId"`} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
}

// TestEntitlementsUpdatePrintsUnprojectedResponse pins that a session-wide
// C1I_FIELDS does not blank the printed entitlement.
func TestEntitlementsUpdatePrintsUnprojectedResponse(t *testing.T) {
	var path string
	var body map[string]any
	srv := captureServer(t, &path, &body)
	orig := viper.GetString("fields")
	viper.Set("fields", "nomatch")
	t.Cleanup(func() { viper.Set("fields", orig) })
	out, err := runEntitlementsUpdate(t, srv, false, "ent-1", withAppID(map[string]string{"description": "d"}))
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !strings.Contains(out, `"id": "ent-1"`) {
		t.Errorf("output = %q, want the unprojected entitlement", out)
	}
}

// TestEntitlementPolicyBindingValidation pins the client-side policy check on
// entitlements update and create: a missing, deleted, or wrong-type policy is
// exit 2 with no mutation sent, and the emergency slot takes a grant policy.
func TestEntitlementPolicyBindingValidation(t *testing.T) {
	policies := map[string]string{
		"gp": "POLICY_TYPE_GRANT", "rp": "POLICY_TYPE_REVOKE", "cp": "POLICY_TYPE_CERTIFY",
		"del": "deleted:POLICY_TYPE_GRANT", "np": "nopolicy",
	}
	upd := []string{"entitlements", "update", "ent-1", "--app-id", "app-1"}
	crt := []string{"entitlements", "create", "--app-id", "app-1", "--display-name", "e", "--resource-type-id", "rt-1", "--resource-id", "res-1"}
	with := func(base []string, extra ...string) []string { return append(append([]string(nil), base...), extra...) }
	cases := []struct {
		name        string
		args        []string
		dryRun      bool
		wantErr     string
		wantExit    int // when wantErr is set; 0 means exitUsage
		wantLookups int
		wantPosts   int
		wantReq     string // a substring of some request line
	}{
		{name: "update 200 without policy is a C1 fault", args: with(upd, "--emergency-grant-policy-id", "np"), wantErr: "no policy", wantExit: exitServer, wantLookups: 1},
		{name: "create 200 without policy is a C1 fault", args: with(crt, "--emergency-grant-policy-id", "np"), wantErr: "no policy", wantExit: exitServer, wantLookups: 1},
		{name: "update trims policy id", args: with(upd, "--emergency-grant-policy-id", " gp ", "--emergency-grant-enabled"), wantLookups: 1, wantPosts: 1, wantReq: `"emergencyGrantPolicyId":"gp"`},
		{name: "create trims policy id", args: with(crt, "--emergency-grant-policy-id", "\tgp\n"), wantLookups: 1, wantPosts: 1, wantReq: `"emergencyGrantPolicyId":"gp"`},
		{name: "update unbind accepts underscore", args: with(upd, "--unbind-policy", "EMERGENCY_GRANT"), wantPosts: 1, wantReq: `"emergencyGrantPolicyId":""`},
		{name: "update missing", args: with(upd, "--grant-policy-id", "nope"), wantErr: `--grant-policy-id "nope": no such policy`, wantLookups: 1},
		{name: "update deleted", args: with(upd, "--certify-policy-id", "del"), wantErr: "policy is deleted", wantLookups: 1},
		{name: "update wrong grant", args: with(upd, "--grant-policy-id", "rp"), wantErr: "it must be POLICY_TYPE_GRANT", wantLookups: 1},
		{name: "update wrong revoke", args: with(upd, "--revoke-policy-id", "gp"), wantErr: "it must be POLICY_TYPE_REVOKE", wantLookups: 1},
		{name: "update wrong certify", args: with(upd, "--certify-policy-id", "gp"), wantErr: "it must be POLICY_TYPE_CERTIFY", wantLookups: 1},
		{name: "update emergency revoke", args: with(upd, "--emergency-grant-policy-id", "rp"), wantErr: `--emergency-grant-policy-id "rp" is a POLICY_TYPE_REVOKE policy; it must be POLICY_TYPE_GRANT`, wantLookups: 1},
		{name: "update emergency certify", args: with(upd, "--emergency-grant-policy-id", "cp"), wantErr: "it must be POLICY_TYPE_GRANT", wantLookups: 1},
		{name: "update all right", args: with(upd, "--grant-policy-id", "gp", "--revoke-policy-id", "rp", "--certify-policy-id", "cp", "--emergency-grant-policy-id", "gp"), wantLookups: 3, wantPosts: 1},
		{name: "update unbind not looked up", args: with(upd, "--unbind-policy", "emergency-grant"), wantPosts: 1},
		{name: "update dry run", args: with(upd, "--grant-policy-id", "nope"), dryRun: true},
		{name: "create missing", args: with(crt, "--revoke-policy-id", "nope"), wantErr: "no such policy", wantLookups: 1},
		{name: "create emergency certify", args: with(crt, "--emergency-grant-policy-id", "cp"), wantErr: "it must be POLICY_TYPE_GRANT", wantLookups: 1},
		{name: "create all right", args: with(crt, "--grant-policy-id", "gp", "--revoke-policy-id", "rp", "--certify-policy-id", "cp", "--emergency-grant-policy-id", "gp"), wantLookups: 3, wantPosts: 1},
		{name: "create dry run", args: with(crt, "--grant-policy-id", "nope"), dryRun: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, requests := policyBindingServer(t, policies)
			resetCmds(t, entitlementsUpdateCmd, entitlementsCreateCmd)
			stubCatalogClient(t, srv)
			withCatalogDryRun(t, tc.dryRun)
			t.Setenv("C1I_URL", "https://example.invalid")

			err := runRootWithArgs(t, tc.args)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("run: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tc.wantErr)
				}
				want := tc.wantExit
				if want == 0 {
					want = exitUsage
				}
				if got := exitCode(err); got != want {
					t.Errorf("exit = %d, want %d", got, want)
				}
			}
			reqs := requests()
			if tc.wantReq != "" && !strings.Contains(strings.Join(reqs, "\n"), tc.wantReq) {
				t.Errorf("no request contains %s: %v", tc.wantReq, reqs)
			}
			if got := countRequests(reqs, "GET /api/v1/policies/"); got != tc.wantLookups {
				t.Errorf("policy lookups = %d, want %d (%v)", got, tc.wantLookups, reqs)
			}
			if got := countRequests(reqs, "POST "); got != tc.wantPosts {
				t.Errorf("mutations = %d, want %d (%v)", got, tc.wantPosts, reqs)
			}
		})
	}
}

// TestEntitlementCreateValidatesBeforeCreatingAnything pins that a bad policy
// is refused before step 1, so no resource type or resource is left behind.
func TestEntitlementCreateValidatesBeforeCreatingAnything(t *testing.T) {
	srv, requests := policyBindingServer(t, map[string]string{"rp": "POLICY_TYPE_REVOKE"})
	resetCmds(t, entitlementsCreateCmd)
	stubCatalogClient(t, srv)
	withCatalogDryRun(t, false)
	t.Setenv("C1I_URL", "https://example.invalid")

	err := runRootWithArgs(t, []string{"entitlements", "create", "--app-id", "app-1", "--display-name", "e", "--grant-policy-id", "rp"})
	if exitCode(err) != exitUsage {
		t.Fatalf("err = %v (exit %d), want exit 2", err, exitCode(err))
	}
	if n := countRequests(requests(), "POST "); n != 0 {
		t.Errorf("%d creates sent before the policy check failed: %v", n, requests())
	}
}

// TestEmptyPolicyIDHintMatchesCommand pins that the empty-policy-id error
// suggests --unbind-policy only on commands that define it.
func TestEmptyPolicyIDHintMatchesCommand(t *testing.T) {
	cases := []struct {
		args     []string
		wantHint bool
	}{
		{[]string{"entitlements", "update", "ent-1", "--app-id", "a", "--revoke-policy-id", ""}, true},
		{[]string{"apps", "update", "app-1", "--revoke-policy-id", ""}, true},
		{[]string{"entitlements", "create", "--app-id", "a", "--display-name", "e", "--revoke-policy-id", ""}, false},
		{[]string{"apps", "create", "--display-name", "a", "--revoke-policy-id", ""}, false},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args[:2], " "), func(t *testing.T) {
			srv, requests := policyBindingServer(t, nil)
			resetCmds(t, entitlementsUpdateCmd, entitlementsCreateCmd, appsUpdateCmd, appsCreateCmd)
			stubCatalogClient(t, srv)
			withCatalogDryRun(t, false)
			t.Setenv("C1I_URL", "https://example.invalid")

			err := runRootWithArgs(t, tc.args)
			if err == nil || exitCode(err) != exitUsage || !strings.Contains(err.Error(), "--revoke-policy-id requires a non-empty value") {
				t.Fatalf("err = %v (exit %d), want the empty-id usage error", err, exitCode(err))
			}
			if got := strings.Contains(err.Error(), "--unbind-policy"); got != tc.wantHint {
				t.Errorf("hint present = %v, want %v: %v", got, tc.wantHint, err)
			}
			if len(requests()) != 0 {
				t.Errorf("requests sent: %v", requests())
			}
		})
	}
}

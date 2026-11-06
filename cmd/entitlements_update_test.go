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

// captureServer records the one request it gets and answers with an
// entitlement envelope.
func captureServer(t *testing.T, gotPath *string, gotBody *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		{"grant policy cleared", map[string]string{"grant-policy-id": ""}, map[string]any{"grantPolicyId": ""}, "grantPolicyId"},
		{"revoke policy cleared", map[string]string{"revoke-policy-id": ""}, map[string]any{"revokePolicyId": ""}, "revokePolicyId"},
		{"certify policy cleared", map[string]string{"certify-policy-id": ""}, map[string]any{"certifyPolicyId": ""}, "certifyPolicyId"},
		{"emergency policy cleared", map[string]string{"emergency-grant-policy-id": ""}, map[string]any{"emergencyGrantPolicyId": ""}, "emergencyGrantPolicyId"},
		{"emergency disabled", map[string]string{"emergency-grant-enabled": "false"}, map[string]any{"emergencyGrantEnabled": false}, "emergencyGrantEnabled"},
		{
			"emergency enabled with policy",
			map[string]string{"emergency-grant-enabled": "true", "emergency-grant-policy-id": "pol-e"},
			map[string]any{"emergencyGrantEnabled": true, "emergencyGrantPolicyId": "pol-e"},
			"emergencyGrantPolicyId,emergencyGrantEnabled",
		},
		{"duration go form", map[string]string{"duration-grant": "1h30m"}, map[string]any{"durationGrant": "5400s"}, "durationGrant"},
		{"duration seconds form", map[string]string{"duration-grant": "3600s"}, map[string]any{"durationGrant": "3600s"}, "durationGrant"},
		{"duration fractional", map[string]string{"duration-grant": "1.5s"}, map[string]any{"durationGrant": "1.5s"}, "durationGrant"},
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
		{"duration grant and unset", map[string]string{"duration-grant": "1h", "duration-unset": "true"}, "mutually exclusive"},
		{"duration unset false", map[string]string{"duration-unset": "false"}, "--duration-unset"},
		{"duration unparseable", map[string]string{"duration-grant": "soon"}, "--duration-grant"},
		{"duration zero", map[string]string{"duration-grant": "0s"}, "must be positive"},
		{"duration negative", map[string]string{"duration-grant": "-1m"}, "must be positive"},
		{"emergency enabled without policy", map[string]string{"emergency-grant-enabled": "true"}, "emergency grant policy id must be set if emergency grant is enabled"},
		{"emergency enabled with cleared policy", map[string]string{"emergency-grant-enabled": "true", "emergency-grant-policy-id": ""}, "emergency grant policy id must be set if emergency grant is enabled"},
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
	for _, want := range []string{"[dry-run] POST /api/v1/apps/app-1/entitlements/ent-1\n", `"grantPolicyId": "pol-g"`, `"updateMask": "grantPolicyId"`} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
}

// TestEntitlementsUpdatePrintsRawResponse pins writeRawObject: a session-wide
// --fields must not blank the confirmation.
func TestEntitlementsUpdatePrintsRawResponse(t *testing.T) {
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
		t.Errorf("output = %q, want the raw response", out)
	}
}

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testMapperJSON = `{"mappingCases":[{"userKeyCel":"subject.email","appUserKeyCel":"account.email"}]}`

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestBuildAppUpdateBodyExactPatchAndMask pins, per flag and in combination,
// that only the supplied fields reach the patch and the mask.
func TestBuildAppUpdateBodyExactPatchAndMask(t *testing.T) {
	mapperFile := writeTempFile(t, testMapperJSON)
	var mapper map[string]any
	_ = json.Unmarshal([]byte(testMapperJSON), &mapper)

	cases := []struct {
		name      string
		flags     [][2]string
		wantApp   map[string]any
		wantMask  string
		wantError string
	}{
		{name: "display name", flags: [][2]string{{"display-name", "New"}},
			wantApp: map[string]any{"displayName": "New"}, wantMask: "displayName"},
		{name: "empty description clears", flags: [][2]string{{"description", ""}},
			wantApp: map[string]any{"description": ""}, wantMask: "description"},
		{name: "grant policy", flags: [][2]string{{"grant-policy-id", "gp"}},
			wantApp: map[string]any{"grantPolicyId": "gp"}, wantMask: "grantPolicyId"},
		{name: "revoke policy", flags: [][2]string{{"revoke-policy-id", "rp"}},
			wantApp: map[string]any{"revokePolicyId": "rp"}, wantMask: "revokePolicyId"},
		{name: "certify policy", flags: [][2]string{{"certify-policy-id", "cp"}},
			wantApp: map[string]any{"certifyPolicyId": "cp"}, wantMask: "certifyPolicyId"},
		{name: "unbind policy", flags: [][2]string{{"unbind-policy", "grant"}},
			wantApp: map[string]any{"grantPolicyId": ""}, wantMask: "grantPolicyId"},
		{name: "empty policy id is refused", flags: [][2]string{{"grant-policy-id", ""}}, wantError: "--grant-policy-id requires a non-empty value"},
		{name: "identity matching short", flags: [][2]string{{"identity-matching", "STRICT"}},
			wantApp: map[string]any{"identityMatching": "APP_USER_IDENTITY_MATCHING_STRICT"}, wantMask: "identityMatching"},
		{name: "identity matching underscore separator", flags: [][2]string{{"identity-matching", "display_name"}},
			wantApp: map[string]any{"identityMatching": "APP_USER_IDENTITY_MATCHING_DISPLAY_NAME"}, wantMask: "identityMatching"},
		{name: "identity matching full enum with dashes", flags: [][2]string{{"identity-matching", "app-user-identity-matching-display-name"}},
			wantApp: map[string]any{"identityMatching": "APP_USER_IDENTITY_MATCHING_DISPLAY_NAME"}, wantMask: "identityMatching"},
		{name: "identity matching full enum", flags: [][2]string{{"identity-matching", "app_user_identity_matching_custom"}},
			wantApp: map[string]any{"identityMatching": "APP_USER_IDENTITY_MATCHING_CUSTOM"}, wantMask: "identityMatching"},
		{name: "mapper", flags: [][2]string{{"app-user-mapper-file", mapperFile}},
			wantApp: map[string]any{"appUserMapper": mapper}, wantMask: "appUserMapper"},
		{name: "combination", flags: [][2]string{
			{"app-user-mapper-file", mapperFile}, {"identity-matching", "display-name"},
			{"certify-policy-id", "cp"}, {"grant-policy-id", "gp"}, {"display-name", "N"},
		},
			wantApp: map[string]any{
				"displayName": "N", "grantPolicyId": "gp", "certifyPolicyId": "cp",
				"identityMatching": "APP_USER_IDENTITY_MATCHING_DISPLAY_NAME", "appUserMapper": mapper,
			},
			wantMask: "displayName,grantPolicyId,certifyPolicyId,identityMatching,appUserMapper"},
		{name: "unknown enum", flags: [][2]string{{"identity-matching", "fuzzy"}}, wantError: "invalid --identity-matching"},
		{name: "unspecified is refused", flags: [][2]string{{"identity-matching", "unspecified"}}, wantError: "invalid --identity-matching"},
		{name: "unspecified full enum is refused", flags: [][2]string{{"identity-matching", "APP_USER_IDENTITY_MATCHING_UNSPECIFIED"}}, wantError: "invalid --identity-matching"},
		{name: "empty mapper path", flags: [][2]string{{"app-user-mapper-file", ""}}, wantError: "requires a path"},
		{name: "nothing", wantError: "nothing to update"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetCmdFlags(t, appsUpdateCmd)
			for _, f := range tc.flags {
				if err := appsUpdateCmd.Flags().Set(f[0], f[1]); err != nil {
					t.Fatalf("set --%s: %v", f[0], err)
				}
			}
			got, err := buildAppUpdateBody(appsUpdateCmd, "app-1")
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("err = %v, want it to contain %q", err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tc.wantApp["id"] = "app-1"
			want := map[string]any{"app": tc.wantApp, "updateMask": tc.wantMask}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("body = %#v\nwant   %#v", got, want)
			}
		})
	}
}

// TestReadAppUserMapperFileShape covers the structural checks, and that an
// unknown key passes through for the server to judge.
func TestReadAppUserMapperFileShape(t *testing.T) {
	cases := []struct {
		name, content, wantErr string
	}{
		{"valid", testMapperJSON, ""},
		{"unknown key passes through", `{"mappingCases":[{"userKeyCel":"a","appUserKeyCel":"b"}],"extra":1}`, ""},
		{"not json", `{`, "must be a JSON object"},
		{"array", `[]`, "must be a JSON object"},
		{"null", `null`, "must be a JSON object"},
		{"missing cases", `{}`, "non-empty array"},
		{"empty cases", `{"mappingCases":[]}`, "non-empty array"},
		{"cases not array", `{"mappingCases":{}}`, "non-empty array"},
		{"case not object", `{"mappingCases":["x"]}`, "mappingCases[0] must be an object"},
		{"cel not string", `{"mappingCases":[{"userKeyCel":1}]}`, "mappingCases[0].userKeyCel must be a string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readAppUserMapperFile(appsUpdateCmd, writeTempFile(t, tc.content))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				var want map[string]any
				_ = json.Unmarshal([]byte(tc.content), &want)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("mapper = %v, want verbatim %v", got, want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// TestAppsUpdateWire drives the real command tree against httptest: escaped
// path, wrapped body, derived mask, and a mapper read from stdin.
func TestAppsUpdateWire(t *testing.T) {
	const appID = "app/id ?#"
	var gotBody map[string]any
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/api/v1/apps/app%2Fid%20%3F%23" {
			t.Errorf("request = %s %s, want POST escaped app path", r.Method, r.URL.EscapedPath())
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &gotBody); err != nil {
			t.Errorf("unmarshal body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"app":{"id":"x"}}`)
	}))
	defer srv.Close()

	resetCmdFlags(t, appsUpdateCmd)
	stubCatalogClient(t, srv)
	withCatalogDryRun(t, false)
	t.Setenv("C1I_URL", "https://example.invalid")
	appsUpdateCmd.SetIn(strings.NewReader(testMapperJSON))
	t.Cleanup(func() { appsUpdateCmd.SetIn(nil) })

	err := runRootWithArgs(t, []string{"apps", "update", appID,
		"--unbind-policy", "revoke", "--identity-matching", "custom", "--app-user-mapper-file", "-"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if hits != 1 {
		t.Fatalf("hits = %d, want 1", hits)
	}
	var mapper map[string]any
	_ = json.Unmarshal([]byte(testMapperJSON), &mapper)
	want := map[string]any{
		"app": map[string]any{
			"id": appID, "revokePolicyId": "",
			"identityMatching": "APP_USER_IDENTITY_MATCHING_CUSTOM", "appUserMapper": mapper,
		},
		"updateMask": "revokePolicyId,identityMatching,appUserMapper",
	}
	if !reflect.DeepEqual(gotBody, want) {
		t.Errorf("body = %#v\nwant   %#v", gotBody, want)
	}
}

// TestAppsUpdateDryRunSendsNothing pins that --dry-run previews the body and
// never reaches the server.
func TestAppsUpdateDryRunSendsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("dry run sent %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	resetCmdFlags(t, appsUpdateCmd)
	stubCatalogClient(t, srv)
	withCatalogDryRun(t, true)
	t.Setenv("C1I_URL", "https://example.invalid")

	var out bytes.Buffer
	appsUpdateCmd.SetOut(&out)
	appsUpdateCmd.SetContext(context.Background())
	t.Cleanup(func() { appsUpdateCmd.SetOut(nil) })
	_ = appsUpdateCmd.Flags().Set("grant-policy-id", "gp-1")
	if err := appsUpdateCmd.RunE(appsUpdateCmd, []string{"app-1"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !strings.Contains(out.String(), `"url": "https://example.invalid/api/v1/apps/app-1"`) || !strings.Contains(out.String(), `"updateMask": "grantPolicyId"`) {
		t.Errorf("dry-run output = %q", out.String())
	}
}

// TestAppsCreateWirePolicyFlags drives create through the command tree and
// pins the top-level policy and identity-matching keys on the wire.
func TestAppsCreateWirePolicyFlags(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := strings.CutPrefix(r.URL.Path, "/api/v1/policies/"); ok {
			typ := map[string]string{"gp": "GRANT", "rp": "REVOKE", "cp": "CERTIFY"}[id]
			_, _ = fmt.Fprintf(w, `{"policy":{"policyType":"POLICY_TYPE_%s"}}`, typ)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/apps" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"app":{"id":"x"}}`)
	}))
	defer srv.Close()

	resetCmdFlags(t, appsCreateCmd)
	stubCatalogClient(t, srv)
	withCatalogDryRun(t, false)
	t.Setenv("C1I_URL", "https://example.invalid")

	err := runRootWithArgs(t, []string{"apps", "create", "--display-name", "A",
		"--grant-policy-id", "gp", "--revoke-policy-id", "rp", "--certify-policy-id", "cp", "--identity-matching", "strict"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := map[string]any{
		"displayName": "A", "grantPolicyId": "gp", "revokePolicyId": "rp", "certifyPolicyId": "cp",
		"identityMatching": "APP_USER_IDENTITY_MATCHING_STRICT",
	}
	if !reflect.DeepEqual(gotBody, want) {
		t.Errorf("body = %#v\nwant   %#v", gotBody, want)
	}
}

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func stubAccessProfileEntitlementsClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := newClient
	newClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newClient = orig })
}

func TestCatalogEntitlementReferencesBuildAppScopedWireBody(t *testing.T) {
	resetCmdFlags(t, accessProfilesRequestableEntitlementsAddCmd)
	if err := accessProfilesRequestableEntitlementsAddCmd.Flags().Set("app-id", "app-1"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ent-1", "ent-2"} {
		if err := accessProfilesRequestableEntitlementsAddCmd.Flags().Set("entitlement-id", id); err != nil {
			t.Fatal(err)
		}
	}
	if err := accessProfilesRequestableEntitlementsAddCmd.Flags().Set("create-requests", "true"); err != nil {
		t.Fatal(err)
	}

	refs, err := catalogEntitlementReferences(accessProfilesRequestableEntitlementsAddCmd, true)
	if err != nil {
		t.Fatalf("catalogEntitlementReferences: %v", err)
	}
	body := buildRequestableEntitlementsBody(accessProfilesRequestableEntitlementsAddCmd, refs)
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	const want = `{"appEntitlements":[{"appId":"app-1","id":"ent-1"},{"appId":"app-1","id":"ent-2"}],"createRequests":true}`
	if string(data) != want {
		t.Errorf("body = %s, want %s", data, want)
	}
}

func TestCatalogEntitlementReferencesRequireAtLeastOneForAddAndRemove(t *testing.T) {
	commands := []*cobra.Command{
		accessProfilesRequestableEntitlementsAddCmd,
		accessProfilesRequestableEntitlementsRemoveCmd,
		accessProfilesVisibilityEntitlementsAddCmd,
		accessProfilesVisibilityEntitlementsRemoveCmd,
	}
	for _, cmd := range commands {
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			resetCmdFlags(t, cmd)
			if err := cmd.Flags().Set("app-id", "app-1"); err != nil {
				t.Fatal(err)
			}
			cmd.SetContext(context.Background())
			err := cmd.RunE(cmd, []string{"catalog-1"})
			if err == nil {
				t.Fatal("expected a missing entitlement reference to fail")
			}
			var ue *usageError
			if !errors.As(err, &ue) {
				t.Errorf("error %v is not a usage error", err)
			}
			if !strings.Contains(err.Error(), "--entitlement-id") {
				t.Errorf("error = %q, want it to name --entitlement-id", err)
			}
		})
	}
}

func TestCatalogEntitlementSetReadsFullReplacementFile(t *testing.T) {
	refsFile := writeJSONFile(t, "requestable-refs.json", `[
		{"appId":"app-1","id":"ent-1"},
		{"appId":"app-2","id":"ent-2"}
	]`)
	resetCmdFlags(t, accessProfilesRequestableEntitlementsSetCmd)
	if err := accessProfilesRequestableEntitlementsSetCmd.Flags().Set("refs-file", refsFile); err != nil {
		t.Fatal(err)
	}

	refs, err := catalogEntitlementReferencesFromFile(accessProfilesRequestableEntitlementsSetCmd)
	if err != nil {
		t.Fatalf("catalogEntitlementReferencesFromFile: %v", err)
	}
	body := buildRequestableEntitlementsBody(accessProfilesRequestableEntitlementsSetCmd, refs)
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	const want = `{"appEntitlements":[{"appId":"app-1","id":"ent-1"},{"appId":"app-2","id":"ent-2"}]}`
	if string(data) != want {
		t.Errorf("body = %s, want %s", data, want)
	}
}

func TestCatalogEntitlementSetRejectsNullReferenceFile(t *testing.T) {
	refsFile := writeJSONFile(t, "requestable-refs.json", `null`)
	resetCmdFlags(t, accessProfilesRequestableEntitlementsSetCmd)
	if err := accessProfilesRequestableEntitlementsSetCmd.Flags().Set("refs-file", refsFile); err != nil {
		t.Fatal(err)
	}

	_, err := catalogEntitlementReferencesFromFile(accessProfilesRequestableEntitlementsSetCmd)
	if err == nil || !strings.Contains(err.Error(), "--refs-file must contain a JSON array, not null") {
		t.Errorf("error = %v, want a JSON-array usage error", err)
	}
}

func TestRequestableEntitlementsAddMapsTypeMismatch400s(t *testing.T) {
	cases := []struct {
		name       string
		apiBody    string
		wantSubstr string
		wantAbsent string
	}{
		{
			name:       "C1AppEntitlements",
			apiBody:    `{"message":"cannot add C1 app entitlements to a catalog","code":3}`,
			wantSubstr: "entitlement type mismatch",
			wantAbsent: "to a catalog",
		},
		{
			name:       "RoleMembership",
			apiBody:    `{"message":"Role membership entitlements can only be added to Catalog profiles","code":3}`,
			wantSubstr: "Catalog access profiles",
			wantAbsent: "Catalog profiles",
		},
		{
			name:       "CatalogHybridMembership",
			apiBody:    `{"message":"membership entitlements of Catalog and Hybrid access profiles cannot be added to a Catalog profile; access profile is not a Role profile","code":3}`,
			wantSubstr: "Role access profile",
			wantAbsent: "is not a Role profile",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(tc.apiBody))
			}))
			defer srv.Close()

			stubAccessProfileEntitlementsClient(t, srv)
			resetCmdFlags(t, accessProfilesRequestableEntitlementsAddCmd)
			t.Setenv("C1I_URL", "https://example.invalid")
			clearViperOverrides(t, "dry_run")
			viper.Set("dry_run", false)
			if err := accessProfilesRequestableEntitlementsAddCmd.Flags().Set("app-id", "app-1"); err != nil {
				t.Fatal(err)
			}
			if err := accessProfilesRequestableEntitlementsAddCmd.Flags().Set("entitlement-id", "ent-1"); err != nil {
				t.Fatal(err)
			}
			accessProfilesRequestableEntitlementsAddCmd.SetContext(context.Background())
			err := accessProfilesRequestableEntitlementsAddCmd.RunE(accessProfilesRequestableEntitlementsAddCmd, []string{"profile-1"})
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			var ue *usageError
			if !errors.As(err, &ue) {
				t.Errorf("error is not a usageError: %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantSubstr)
			}
			if strings.Contains(err.Error(), tc.wantAbsent) {
				t.Errorf("error = %q, should NOT contain raw API text %q", err.Error(), tc.wantAbsent)
			}
			if !strings.Contains(err.Error(), "access-profiles get profile-1") {
				t.Errorf("error = %q, want it to suggest 'access-profiles get profile-1'", err.Error())
			}
		})
	}
}

func TestRequestableEntitlementsAddPassesThroughUnknown400s(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"some other validation error","code":3}`))
	}))
	defer srv.Close()

	stubAccessProfileEntitlementsClient(t, srv)
	resetCmdFlags(t, accessProfilesRequestableEntitlementsAddCmd)
	t.Setenv("C1I_URL", "https://example.invalid")
	clearViperOverrides(t, "dry_run")
	viper.Set("dry_run", false)
	if err := accessProfilesRequestableEntitlementsAddCmd.Flags().Set("app-id", "app-1"); err != nil {
		t.Fatal(err)
	}
	if err := accessProfilesRequestableEntitlementsAddCmd.Flags().Set("entitlement-id", "ent-1"); err != nil {
		t.Fatal(err)
	}
	accessProfilesRequestableEntitlementsAddCmd.SetContext(context.Background())
	err := accessProfilesRequestableEntitlementsAddCmd.RunE(accessProfilesRequestableEntitlementsAddCmd, []string{"profile-1"})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	// The raw API error should pass through unchanged for unknown 400s.
	if !strings.Contains(err.Error(), "some other validation error") {
		t.Errorf("error = %q, want it to contain the raw API message for unknown 400s", err.Error())
	}
}

func TestCatalogEntitlementDeleteSendsJSONBodyAndEscapedProfileID(t *testing.T) {
	const profileID = "profile /?#"
	const wantEscapedPath = "/api/v1/catalogs/profile%20%2F%3F%23/requestable_entries"

	var gotMethod, gotPath, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.EscapedPath()
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"removed":true}`))
	}))
	defer srv.Close()

	stubAccessProfileEntitlementsClient(t, srv)
	resetCmdFlags(t, accessProfilesRequestableEntitlementsRemoveCmd)
	t.Setenv("C1I_URL", "https://example.invalid")
	clearViperOverrides(t, "dry_run")
	viper.Set("dry_run", false)
	if err := accessProfilesRequestableEntitlementsRemoveCmd.Flags().Set("app-id", "app-1"); err != nil {
		t.Fatal(err)
	}
	if err := accessProfilesRequestableEntitlementsRemoveCmd.Flags().Set("entitlement-id", "ent-1"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	restoreCmdWriters(t)
	accessProfilesRequestableEntitlementsRemoveCmd.SetOut(&out)
	accessProfilesRequestableEntitlementsRemoveCmd.SetContext(context.Background())
	if err := accessProfilesRequestableEntitlementsRemoveCmd.RunE(accessProfilesRequestableEntitlementsRemoveCmd, []string{profileID}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != wantEscapedPath {
		t.Errorf("escaped path = %q, want %q", gotPath, wantEscapedPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	const wantBody = `{"appEntitlements":[{"appId":"app-1","id":"ent-1"}]}`
	if string(gotBody) != wantBody {
		t.Errorf("body = %s, want %s", gotBody, wantBody)
	}
	if !strings.Contains(out.String(), `"removed": true`) {
		t.Errorf("mutation response was not preserved: %q", out.String())
	}
}

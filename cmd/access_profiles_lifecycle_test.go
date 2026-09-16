package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newCatalogLifecycleFlagCmd() *cobra.Command {
	cmd := &cobra.Command{}
	f := cmd.Flags()
	f.String("display-name", "", "")
	f.String("description", "", "")
	f.Bool("published", false, "")
	f.Bool("visible-to-everyone", false, "")
	f.Bool("request-bundle", false, "")
	f.String("enrollment-behavior", "", "")
	f.String("unenrollment-behavior", "", "")
	f.String("unenrollment-entitlement-behavior", "", "")
	return cmd
}

func resetCatalogLifecycleFlags(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	reset := func() {
		for _, name := range []string{
			"display-name", "description", "enrollment-behavior", "unenrollment-behavior", "unenrollment-entitlement-behavior",
		} {
			f := cmd.Flags().Lookup(name)
			_ = f.Value.Set("")
			f.Changed = false
		}
		for _, name := range []string{"published", "visible-to-everyone", "request-bundle"} {
			f := cmd.Flags().Lookup(name)
			_ = f.Value.Set("false")
			f.Changed = false
		}
	}
	reset()
	t.Cleanup(reset)
}

func stubCatalogClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := newClient
	newClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newClient = orig })
}

func withCatalogDryRun(t *testing.T, enabled bool) {
	t.Helper()
	orig := viper.GetBool("dry_run")
	viper.Set("dry_run", enabled)
	t.Cleanup(func() { viper.Set("dry_run", orig) })
}

// TestBuildAccessProfileCreateBodyAllWritableFields verifies that create sends
// every scalar field and preserves explicit false values alongside enum values.
func TestBuildAccessProfileCreateBodyAllWritableFields(t *testing.T) {
	cmd := newCatalogLifecycleFlagCmd()
	for flag, value := range map[string]string{
		"display-name":                      "Engineering",
		"description":                       "eng access",
		"published":                         "false",
		"visible-to-everyone":               "true",
		"request-bundle":                    "false",
		"enrollment-behavior":               "bypass",
		"unenrollment-behavior":             "revoke-unjustified",
		"unenrollment-entitlement-behavior": "enforce",
	} {
		if err := cmd.Flags().Set(flag, value); err != nil {
			t.Fatalf("set --%s: %v", flag, err)
		}
	}

	got := buildAccessProfileCreateBody(cmd)
	want := map[string]any{
		"displayName":                     "Engineering",
		"description":                     "eng access",
		"published":                       false,
		"visibleToEveryone":               true,
		"requestBundle":                   false,
		"enrollmentBehavior":              "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_BYPASS_ENTITLEMENT_REQUEST_POLICY",
		"unenrollmentBehavior":            "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_REVOKE_UNJUSTIFIED",
		"unenrollmentEntitlementBehavior": "REQUEST_CATALOG_UNENROLLMENT_ENTITLEMENT_BEHAVIOR_ENFORCE",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %#v, want %#v", got, want)
	}
}

// TestBuildAccessProfileUpdateBodyPreservesClearsAndExactMask proves omitted
// fields stay out of both the patch and its mask while explicit empty/false do not.
func TestBuildAccessProfileUpdateBodyPreservesClearsAndExactMask(t *testing.T) {
	cmd := newCatalogLifecycleFlagCmd()
	for flag, value := range map[string]string{
		"description":                       "",
		"published":                         "false",
		"enrollment-behavior":               "enforce",
		"unenrollment-entitlement-behavior": "bypass",
	} {
		if err := cmd.Flags().Set(flag, value); err != nil {
			t.Fatalf("set --%s: %v", flag, err)
		}
	}

	got, err := buildAccessProfileUpdateBody(cmd, "cat-1")
	if err != nil {
		t.Fatalf("buildAccessProfileUpdateBody: %v", err)
	}
	want := map[string]any{
		"catalog": map[string]any{
			"id":                              "cat-1",
			"description":                     "",
			"published":                       false,
			"enrollmentBehavior":              "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_ENFORCE_ENTITLEMENT_REQUEST_POLICY",
			"unenrollmentEntitlementBehavior": "REQUEST_CATALOG_UNENROLLMENT_ENTITLEMENT_BEHAVIOR_BYPASS",
		},
		"updateMask": "description,enrollmentBehavior,unenrollmentEntitlementBehavior,published",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %#v, want %#v", got, want)
	}
}

// TestAccessProfilesUpdateSendsEscapedWrappedPatch drives the command against
// an httptest server so the escaped path, nested catalog, and derived mask are
// verified at the wire rather than inferred from the body builder.
func TestAccessProfilesUpdateSendsEscapedWrappedPatch(t *testing.T) {
	const catalogID = "catalog/id ?#"
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/api/v1/catalogs/catalog%2Fid%20%3F%23" {
			t.Errorf("request = %s %s, want POST escaped catalog path", r.Method, r.URL.EscapedPath())
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("unmarshal body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	resetCatalogLifecycleFlags(t, accessProfilesUpdateCmd)
	stubCatalogClient(t, srv)
	withCatalogDryRun(t, false)
	t.Setenv("C1I_URL", "https://example.invalid")
	_ = accessProfilesUpdateCmd.Flags().Set("description", "")
	_ = accessProfilesUpdateCmd.Flags().Set("visible-to-everyone", "false")

	var out bytes.Buffer
	accessProfilesUpdateCmd.SetOut(&out)
	accessProfilesUpdateCmd.SetContext(context.Background())
	if err := accessProfilesUpdateCmd.RunE(accessProfilesUpdateCmd, []string{catalogID}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	catalog, ok := gotBody["catalog"].(map[string]any)
	if !ok {
		t.Fatalf("catalog = %#v, want object", gotBody["catalog"])
	}
	if !reflect.DeepEqual(catalog, map[string]any{"id": catalogID, "description": "", "visibleToEveryone": false}) {
		t.Errorf("catalog = %#v", catalog)
	}
	if gotBody["updateMask"] != "description,visibleToEveryone" {
		t.Errorf("updateMask = %#v, want exact changed fields", gotBody["updateMask"])
	}
}

// TestAccessProfilesDeleteSendsEscapedDelete verifies delete reaches only the
// catalog ID path, including reserved characters that must stay within the ID.
func TestAccessProfilesDeleteSendsEscapedDelete(t *testing.T) {
	const catalogID = "catalog/id ?#"
	var received bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		if r.Method != http.MethodDelete || r.URL.EscapedPath() != "/api/v1/catalogs/catalog%2Fid%20%3F%23" {
			t.Errorf("request = %s %s, want DELETE escaped catalog path", r.Method, r.URL.EscapedPath())
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	stubCatalogClient(t, srv)
	withCatalogDryRun(t, false)
	t.Setenv("C1I_URL", "https://example.invalid")

	var out bytes.Buffer
	accessProfilesDeleteCmd.SetOut(&out)
	accessProfilesDeleteCmd.SetContext(context.Background())
	if err := accessProfilesDeleteCmd.RunE(accessProfilesDeleteCmd, []string{catalogID}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !received {
		t.Error("delete did not reach the server")
	}
}

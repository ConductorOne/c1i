package cmd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func writeJSONFile(t *testing.T, name, content string) string {
	t.Helper()
	path := t.TempDir() + "/" + name
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

func enableBundleAutomationDryRun(t *testing.T) {
	t.Helper()
	orig := viper.GetBool("dry_run")
	viper.Set("dry_run", true)
	t.Cleanup(func() { viper.Set("dry_run", orig) })
}

// TestBundleAutomationMutationsDryRun proves all five mutating leaves render
// the exact method, escaped route, and payload that they would send, without
// credentials or a network request. It covers both body-file forms and the
// optional run refs-file form, whose AppEntitlementRef values must stay nested
// under refs rather than being sent as a bare array.
func TestBundleAutomationMutationsDryRun(t *testing.T) {
	bodyFile := writeJSONFile(t, "bundle.json", `{
		"createTasks": true,
		"disableCircuitBreaker": false,
		"enabled": true,
		"entitlements": {"entitlementRefs": [{"appId": "app-1", "id": "ent-1"}]}
	}`)
	refsFile := writeJSONFile(t, "refs.json", `[{"appId": "app-1", "id": "ent-1"}]`)

	t.Setenv("C1I_URL", "https://example.invalid")
	enableBundleAutomationDryRun(t)

	cases := []struct {
		name     string
		cmd      *cobra.Command
		flag     string
		file     string
		method   string
		suffix   string
		contains []string
	}{
		{
			name:     "create",
			cmd:      accessProfilesBundleAutomationCreateCmd,
			flag:     "body-file",
			file:     bodyFile,
			method:   "POST",
			suffix:   "/create",
			contains: []string{`"createTasks": true`, `"disableCircuitBreaker": false`, `"enabled": true`, `"entitlementRefs"`},
		},
		{
			name:     "set",
			cmd:      accessProfilesBundleAutomationSetCmd,
			flag:     "body-file",
			file:     bodyFile,
			method:   "POST",
			contains: []string{`"disableCircuitBreaker": false`, `"enabled": true`},
		},
		{
			name:   "delete",
			cmd:    accessProfilesBundleAutomationDeleteCmd,
			method: "DELETE",
		},
		{
			name:     "resume",
			cmd:      accessProfilesBundleAutomationResumeCmd,
			method:   "POST",
			suffix:   "/resume",
			contains: []string{"{}"},
		},
		{
			name:     "run with refs file",
			cmd:      accessProfilesBundleAutomationRunCmd,
			flag:     "refs-file",
			file:     refsFile,
			method:   "POST",
			suffix:   "/run",
			contains: []string{`"refs"`, `"appId": "app-1"`},
		},
	}

	const accessProfileID = "profile/with space"
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resetCmdFlags(t, tt.cmd)
			if tt.flag != "" {
				if err := tt.cmd.Flags().Set(tt.flag, tt.file); err != nil {
					t.Fatalf("setting --%s: %v", tt.flag, err)
				}
			}

			var out bytes.Buffer
			tt.cmd.SetOut(&out)
			tt.cmd.SetContext(context.Background())
			if err := tt.cmd.RunE(tt.cmd, []string{accessProfileID}); err != nil {
				t.Fatalf("RunE: %v", err)
			}

			wantRequest := fmt.Sprintf("[dry-run] %s /api/v1/catalogs/profile%%2Fwith%%20space/bundle_automation%s", tt.method, tt.suffix)
			if got := out.String(); !strings.Contains(got, wantRequest) {
				t.Errorf("dry-run output %q does not contain request %q", got, wantRequest)
			}
			for _, want := range tt.contains {
				if got := out.String(); !strings.Contains(got, want) {
					t.Errorf("dry-run output %q does not contain %q", got, want)
				}
			}
		})
	}
}

// TestBundleAutomationBodyFileRejectsUndocumentedFields ensures body-file is
// structured input, not the raw-api escape hatch: a typo or a server-managed
// field fails locally rather than being silently forwarded to the API.
func TestBundleAutomationBodyFileRejectsUndocumentedFields(t *testing.T) {
	resetCmdFlags(t, accessProfilesBundleAutomationCreateCmd)
	enableBundleAutomationDryRun(t)

	path := writeJSONFile(t, "invalid-bundle.json", `{"requestCatalogId":"catalog-1"}`)
	if err := accessProfilesBundleAutomationCreateCmd.Flags().Set("body-file", path); err != nil {
		t.Fatalf("setting --body-file: %v", err)
	}
	accessProfilesBundleAutomationCreateCmd.SetContext(context.Background())

	err := accessProfilesBundleAutomationCreateCmd.RunE(accessProfilesBundleAutomationCreateCmd, []string{"catalog-1"})
	if err == nil {
		t.Fatal("RunE unexpectedly accepted an undocumented bundle automation field")
	}
	if !strings.Contains(err.Error(), "unsupported bundle automation field") {
		t.Errorf("error = %q, want it to name the unsupported field", err)
	}
}

func TestBundleAutomationBodyFileValidatesPublishedRequestFields(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "accept published fields",
			body: `{
				"createTasks":true,
				"disableCircuitBreaker":false,
				"enabled":true,
				"entitlements":{"entitlementRefs":[{"appId":"app-1","id":"ent-1"}]}
			}`,
		},
		{name: "reject null body", body: `null`, wantErr: "JSON object"},
		{
			name:    "reject unsupported CEL condition",
			body:    `{"cel":{"expression":"true"}}`,
			wantErr: "unsupported bundle automation field",
		},
		{
			name:    "reject unsupported circuit breaker option",
			body:    `{"enforceOnSmallProfiles":true}`,
			wantErr: "unsupported bundle automation field",
		},
		{
			name:    "reject unsupported threshold",
			body:    `{"removedMembersThresholdPercent":"100"}`,
			wantErr: "unsupported bundle automation field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetCmdFlags(t, accessProfilesBundleAutomationCreateCmd)
			enableBundleAutomationDryRun(t)
			path := writeJSONFile(t, "bundle.json", tt.body)
			if err := accessProfilesBundleAutomationCreateCmd.Flags().Set("body-file", path); err != nil {
				t.Fatal(err)
			}
			accessProfilesBundleAutomationCreateCmd.SetContext(context.Background())
			err := accessProfilesBundleAutomationCreateCmd.RunE(accessProfilesBundleAutomationCreateCmd, []string{"catalog-1"})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("RunE: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestBundleAutomationGetEscapesAccessProfileID drives the read leaf through
// an httptest server so an id containing a slash remains one escaped segment,
// not two path components that address a different catalog.
func TestBundleAutomationGetEscapesAccessProfileID(t *testing.T) {
	const accessProfileID = "profile/with space"
	const wantPath = "/api/v1/catalogs/profile%2Fwith%20space/bundle_automation"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != wantPath {
			t.Errorf("request = %s %s, want GET %s", r.Method, r.URL.EscapedPath(), wantPath)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"enabled":true}`)
	}))
	defer srv.Close()

	origNewClient := newClient
	newClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newClient = origNewClient })

	resetCmdFlags(t, accessProfilesBundleAutomationGetCmd)
	origDryRun := viper.GetBool("dry_run")
	viper.Set("dry_run", false)
	t.Cleanup(func() { viper.Set("dry_run", origDryRun) })
	t.Setenv("C1I_URL", "https://example.invalid")

	var out bytes.Buffer
	accessProfilesBundleAutomationGetCmd.SetOut(&out)
	accessProfilesBundleAutomationGetCmd.SetContext(context.Background())
	if err := accessProfilesBundleAutomationGetCmd.RunE(accessProfilesBundleAutomationGetCmd, []string{accessProfileID}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if got := out.String(); !strings.Contains(got, `"enabled": true`) {
		t.Errorf("output = %q, want unprojected response JSON", got)
	}
}

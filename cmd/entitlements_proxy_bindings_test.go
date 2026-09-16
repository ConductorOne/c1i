package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func setProxyBindingFlags(t *testing.T, cmd *cobra.Command, values map[string]string) {
	t.Helper()
	for name, value := range values {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("setting --%s: %v", name, err)
		}
	}
}

func proxyBindingTestValues() map[string]string {
	return map[string]string{
		"source-app-id":              "source/app ?#",
		"source-entitlement-id":      "source/entitlement ?#",
		"destination-app-id":         "destination/app ?#",
		"destination-entitlement-id": "destination/entitlement ?#",
	}
}

func TestProxyBindingMutationsDryRunUseEscapedDirectionalPath(t *testing.T) {
	origDryRun := viper.GetBool("dry_run")
	viper.Set("dry_run", true)
	t.Cleanup(func() { viper.Set("dry_run", origDryRun) })

	for _, tt := range []struct {
		name   string
		cmd    *cobra.Command
		method string
	}{
		{name: "create", cmd: entitlementsProxyBindingsCreateCmd, method: http.MethodPost},
		{name: "delete", cmd: entitlementsProxyBindingsDeleteCmd, method: http.MethodDelete},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetCmdFlags(t, tt.cmd)
			setProxyBindingFlags(t, tt.cmd, proxyBindingTestValues())

			var out bytes.Buffer
			tt.cmd.SetOut(&out)
			t.Cleanup(func() { tt.cmd.SetOut(nil) })
			tt.cmd.SetContext(context.Background())
			if err := tt.cmd.RunE(tt.cmd, nil); err != nil {
				t.Fatalf("RunE: %v", err)
			}

			want := fmt.Sprintf("[dry-run] %s /api/v1/apps/source%%2Fapp%%20%%3F%%23/source%%2Fentitlement%%20%%3F%%23/bindings/destination%%2Fapp%%20%%3F%%23/destination%%2Fentitlement%%20%%3F%%23", tt.method)
			if !strings.Contains(out.String(), want) {
				t.Errorf("dry-run output %q does not contain %q", out.String(), want)
			}
			if tt.method == http.MethodPost && !strings.Contains(out.String(), "{}") {
				t.Errorf("create dry-run output %q does not contain empty request object", out.String())
			}
		})
	}
}

func TestProxyBindingGetUnwrapsResponseAndUsesDirectionalPath(t *testing.T) {
	const wantPath = "/api/v1/apps/source%2Fapp%20%3F%23/source%2Fentitlement%20%3F%23/bindings/destination%2Fapp%20%3F%23/destination%2Fentitlement%20%3F%23"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != wantPath {
			t.Errorf("request = %s %s, want GET %s", r.Method, r.URL.EscapedPath(), wantPath)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"appProxyEntitlementView":{"appProxyEntitlement":{"srcAppId":"source/app ?#","srcAppEntitlementId":"source/entitlement ?#","dstAppId":"destination/app ?#","dstAppEntitlementId":"destination/entitlement ?#"},"srcAppPath":"apps/source"}}`)
	}))
	defer srv.Close()

	orig := newClient
	newClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newClient = orig })
	origDryRun := viper.GetBool("dry_run")
	viper.Set("dry_run", false)
	t.Cleanup(func() { viper.Set("dry_run", origDryRun) })

	resetCmdFlags(t, entitlementsProxyBindingsGetCmd)
	setProxyBindingFlags(t, entitlementsProxyBindingsGetCmd, proxyBindingTestValues())
	t.Setenv("C1I_URL", "https://example.invalid")
	var out bytes.Buffer
	entitlementsProxyBindingsGetCmd.SetOut(&out)
	t.Cleanup(func() { entitlementsProxyBindingsGetCmd.SetOut(nil) })
	entitlementsProxyBindingsGetCmd.SetContext(context.Background())
	if err := entitlementsProxyBindingsGetCmd.RunE(entitlementsProxyBindingsGetCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if strings.Contains(out.String(), "appProxyEntitlementView") || !strings.Contains(out.String(), `"srcAppId": "source/app ?#"`) {
		t.Errorf("output = %q, want unwrapped proxy binding", out.String())
	}
}

func TestProxyBindingRequiresEveryDirectionalScope(t *testing.T) {
	for _, missing := range []string{
		"source-app-id",
		"source-entitlement-id",
		"destination-app-id",
		"destination-entitlement-id",
	} {
		t.Run(missing, func(t *testing.T) {
			resetCmdFlags(t, entitlementsProxyBindingsCreateCmd)
			values := proxyBindingTestValues()
			delete(values, missing)
			setProxyBindingFlags(t, entitlementsProxyBindingsCreateCmd, values)
			entitlementsProxyBindingsCreateCmd.SetContext(context.Background())
			err := entitlementsProxyBindingsCreateCmd.RunE(entitlementsProxyBindingsCreateCmd, nil)
			if err == nil || exitCode(err) != exitUsage || !strings.Contains(err.Error(), missing) {
				t.Errorf("error = %v, want usage error naming --%s", err, missing)
			}
		})
	}
}

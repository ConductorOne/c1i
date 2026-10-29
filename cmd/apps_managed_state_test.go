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
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func withManagedStateTestClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	origNew, origList := newClient, newListClient
	factory := func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	newClient, newListClient = factory, factory
	t.Cleanup(func() { newClient, newListClient = origNew, origList })
	t.Setenv("C1I_URL", "https://example.invalid")
}

func TestAppsManagedStateListPaginatesAndPreservesViews(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/apps/connector/resource_types/apps/managed_state_bindings" {
			t.Errorf("request = %s %s, unexpected", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			if r.URL.Query().Get("page_size") == "" || r.URL.Query().Get("page_token") != "" {
				t.Errorf("first-page query = %v", r.URL.Query())
			}
			_, _ = io.WriteString(w, `{"list":[{"appManagementStateBinding":{"resourceId":"r1"},"resourcePath":"/r1"}],"nextPageToken":"next"}`)
			return
		}
		if r.URL.Query().Get("page_token") != "next" {
			t.Errorf("second-page token = %q, want next", r.URL.Query().Get("page_token"))
		}
		_, _ = io.WriteString(w, `{"list":[{"appManagementStateBinding":{"resourceId":"r2"},"resourcePath":"/r2"}]}`)
	}))
	defer srv.Close()
	withManagedStateTestClient(t, srv)

	resetCmdFlags(t, appsManagedStateListCmd)
	_ = appsManagedStateListCmd.Flags().Set("app-id", "connector")
	_ = appsManagedStateListCmd.Flags().Set("resource-type-id", "apps")
	var out bytes.Buffer
	appsManagedStateListCmd.SetOut(&out)
	appsManagedStateListCmd.SetContext(context.Background())
	if err := appsManagedStateListCmd.RunE(appsManagedStateListCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	var rows []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("decode NDJSON row %q: %v", line, err)
		}
		rows = append(rows, row)
	}
	if len(rows) != 2 || rows[0]["resourcePath"] != "/r1" || rows[1]["resourcePath"] != "/r2" {
		t.Fatalf("rows = %#v, want both view rows", rows)
	}
}

func TestAppsManagedStateGetUsesNestedPathAndUnwrapsView(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/api/v1/apps/connector/resource_types/type%2F1/managed_state_bindings/resource%20one" {
			t.Errorf("request = %s %s, unexpected", r.Method, r.URL.EscapedPath())
		}
		_, _ = io.WriteString(w, `{"appManagementState":{"appManagementStateBinding":{"resourceId":"resource one"},"resourcePath":"/resource one"},"expanded":[]}`)
	}))
	defer srv.Close()
	withManagedStateTestClient(t, srv)

	resetCmdFlags(t, appsManagedStateGetCmd)
	_ = appsManagedStateGetCmd.Flags().Set("app-id", "connector")
	_ = appsManagedStateGetCmd.Flags().Set("resource-type-id", "type/1")
	var out bytes.Buffer
	appsManagedStateGetCmd.SetOut(&out)
	appsManagedStateGetCmd.SetContext(context.Background())
	if err := appsManagedStateGetCmd.RunE(appsManagedStateGetCmd, []string{"resource one"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if got["resourcePath"] != "/resource one" || got["expanded"] == nil {
		t.Fatalf("output = %#v, want view with expanded sibling", got)
	}
}

func TestAppsManagedStatePromoteSendsOwners(t *testing.T) {
	resetCmdFlags(t, appsManagedStatePromoteCmd)
	_ = appsManagedStatePromoteCmd.Flags().Set("app-id", "connector")
	_ = appsManagedStatePromoteCmd.Flags().Set("resource-type-id", "type")
	_ = appsManagedStatePromoteCmd.Flags().Set("user-id", "user-1")
	_ = appsManagedStatePromoteCmd.Flags().Set("entitlement-owner", "source:entitlement")
	body, err := managedStatePromoteBody(appsManagedStatePromoteCmd)
	if err != nil {
		t.Fatalf("managedStatePromoteBody: %v", err)
	}
	want := map[string]any{
		"userIds":                 []string{"user-1"},
		"appEntitlementOwnerRefs": []map[string]string{{"appId": "source", "id": "entitlement"}},
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %#v, want %#v", body, want)
	}
	wireBytes, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal expected body: %v", err)
	}
	var wantWireBody map[string]any
	if err := json.Unmarshal(wireBytes, &wantWireBody); err != nil {
		t.Fatalf("normalize expected body: %v", err)
	}

	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/apps/connector/resource_types/type/managed_state_bindings/resource/promote" {
			t.Errorf("request = %s %s, unexpected", r.Method, r.URL.Path)
		}
		gotBody := decodeJSONBody(t, r)
		if !reflect.DeepEqual(gotBody, wantWireBody) {
			t.Errorf("request body = %#v, want %#v", gotBody, wantWireBody)
		}
		_, _ = fmt.Fprint(w, `{"appManagementState":{"appManagementStateBinding":{"resourceId":"resource"},"resourcePath":"/resource"}}`)
	}))
	defer srv.Close()
	withManagedStateTestClient(t, srv)
	resetCmdFlags(t, appsManagedStatePromoteCmd)
	_ = appsManagedStatePromoteCmd.Flags().Set("app-id", "connector")
	_ = appsManagedStatePromoteCmd.Flags().Set("resource-type-id", "type")
	_ = appsManagedStatePromoteCmd.Flags().Set("user-id", "user-1")
	_ = appsManagedStatePromoteCmd.Flags().Set("entitlement-owner", "source:entitlement")
	var out bytes.Buffer
	appsManagedStatePromoteCmd.SetOut(&out)
	appsManagedStatePromoteCmd.SetContext(context.Background())
	if err := appsManagedStatePromoteCmd.RunE(appsManagedStatePromoteCmd, []string{"resource"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	var response map[string]any
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if response["resourcePath"] != "/resource" {
		t.Fatalf("output = %#v, want unwrapped view", response)
	}
}

func TestManagedStatePromoteDefaultsToInheritedOwnersAndRejectsMalformedRef(t *testing.T) {
	resetCmdFlags(t, appsManagedStatePromoteCmd)
	body, err := managedStatePromoteBody(appsManagedStatePromoteCmd)
	if err != nil || len(body) != 0 {
		t.Fatalf("default body = %#v, error %v; want empty object to inherit source owners", body, err)
	}
	_ = appsManagedStatePromoteCmd.Flags().Set("entitlement-owner", "missing-separator")
	if _, err := managedStatePromoteBody(appsManagedStatePromoteCmd); err == nil {
		t.Fatal("malformed --entitlement-owner should fail")
	}
}

func TestAppsManagedStatePromoteDryRunPreviewsWithoutSending(t *testing.T) {
	resetCmdFlags(t, appsManagedStatePromoteCmd)
	_ = appsManagedStatePromoteCmd.Flags().Set("app-id", "connector")
	_ = appsManagedStatePromoteCmd.Flags().Set("resource-type-id", "type")
	t.Setenv("C1I_URL", "https://example.invalid")
	original := viper.GetBool("dry_run")
	viper.Set("dry_run", true)
	t.Cleanup(func() { viper.Set("dry_run", original) })

	var out bytes.Buffer
	appsManagedStatePromoteCmd.SetOut(&out)
	appsManagedStatePromoteCmd.SetContext(context.Background())
	if err := appsManagedStatePromoteCmd.RunE(appsManagedStatePromoteCmd, []string{"resource"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	want := "[dry-run] POST /api/v1/apps/connector/resource_types/type/managed_state_bindings/resource/promote"
	if !bytes.Contains(out.Bytes(), []byte(want)) || !bytes.Contains(out.Bytes(), []byte("{}")) {
		t.Fatalf("dry-run output = %q, want path and empty inheritance body", out.String())
	}
}

func TestManagedStateCommandsRejectEmptyResourceIDBeforeRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "get", cmd: appsManagedStateGetCmd},
		{name: "promote", cmd: appsManagedStatePromoteCmd},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
			}))
			defer srv.Close()
			withManagedStateTestClient(t, srv)

			resetCmdFlags(t, tc.cmd)
			_ = tc.cmd.Flags().Set("app-id", "connector")
			_ = tc.cmd.Flags().Set("resource-type-id", "type")
			tc.cmd.SetContext(context.Background())
			err := tc.cmd.RunE(tc.cmd, []string{""})
			var pathErr *client.PathError
			if !errors.As(err, &pathErr) {
				t.Fatalf("RunE error = %v, want shared client PathError", err)
			}
			if requests != 0 {
				t.Fatalf("requests = %d, want no request for an empty resource id", requests)
			}
		})
	}
}

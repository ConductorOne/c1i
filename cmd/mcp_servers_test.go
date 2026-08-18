package cmd

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestMapServerEnums(t *testing.T) {
	cases := []struct {
		fn       func(string) string
		in, want string
	}{
		{mapServerType, "hosted", "MCP_SERVER_TYPE_HOSTED"},
		{mapServerType, "EXTERNAL", "MCP_SERVER_TYPE_EXTERNAL"},
		{mapServerType, "passthrough", "passthrough"}, // unknown passes through
		{mapDataSensitivity, "confidential", "MCP_SERVER_DATA_SENSITIVITY_CONFIDENTIAL"},
		{mapDataSensitivity, "Public", "MCP_SERVER_DATA_SENSITIVITY_PUBLIC"},
		{mapTransportType, "sse", "MCP_SERVER_TRANSPORT_TYPE_SSE"},
		{mapTransportType, "streamable-http", "MCP_SERVER_TRANSPORT_TYPE_STREAMABLE_HTTP"},
		{mapTransportType, "http", "MCP_SERVER_TRANSPORT_TYPE_STREAMABLE_HTTP"},
		{mapTokenSharing, "per-user", "MCP_SERVER_TOKEN_SHARING_PER_USER"},
		{mapTokenSharing, "shared", "MCP_SERVER_TOKEN_SHARING_SHARED"},
	}
	for _, c := range cases {
		if got := c.fn(c.in); got != c.want {
			t.Errorf("map(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestServerRowAndCount(t *testing.T) {
	raw := `{"connectorId":"c1","appId":"a1","displayName":"Datadog","serverType":"MCP_SERVER_TYPE_HOSTED","dataSensitivity":"MCP_SERVER_DATA_SENSITIVITY_INTERNAL","authMethod":"MCP_SERVER_AUTH_METHOD_BEARER_TOKEN","mcpServerCatalogId":"cat1","toolPrefix":"dd","createdAt":"2026-01-02T03:04:05Z"}`
	var v serverView
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	row := serverRow(v)
	want := map[string]string{
		"connector_id":          "c1",
		"app_id":                "a1",
		"display_name":          "Datadog",
		"server_type":           "MCP_SERVER_TYPE_HOSTED",
		"data_sensitivity":      "MCP_SERVER_DATA_SENSITIVITY_INTERNAL",
		"auth_method":           "MCP_SERVER_AUTH_METHOD_BEARER_TOKEN",
		"mcp_server_catalog_id": "cat1",
		"tool_prefix":           "dd",
		"created_at":            "2026-01-02T03:04:05Z",
	}
	for k, w := range want {
		if row[k] != w {
			t.Errorf("row[%q] = %v, want %q", k, row[k], w)
		}
	}
	countRow := serverCountRow(v, 7)
	// tool_count must be a real JSON number (int64(7)), not the string "7" —
	// otherwise a `jq '.tool_count > 5'` pipeline does a string comparison
	// instead of a numeric one.
	if countRow["tool_count"] != int64(7) {
		t.Errorf("tool_count = %v (%T), want int64(7)", countRow["tool_count"], countRow["tool_count"])
	}
	b, err := json.Marshal(countRow)
	if err != nil {
		t.Fatalf("marshal countRow: %v", err)
	}
	if !strings.Contains(string(b), `"tool_count":7`) {
		t.Errorf("marshalled countRow = %s, want a bare numeric tool_count (\"tool_count\":7)", b)
	}
}

// TestFlexInt64 pins that tool counts parse whether the gateway sends them as a
// quoted string (canonical proto3 JSON for int64) or a bare number.
func TestFlexInt64(t *testing.T) {
	cases := map[string]int64{
		`"42"`: 42,
		`42`:   42,
		`""`:   0,
		`null`: 0,
	}
	for in, want := range cases {
		var f flexInt64
		if err := json.Unmarshal([]byte(in), &f); err != nil {
			t.Fatalf("unmarshal %s: %v", in, err)
		}
		if int64(f) != want {
			t.Errorf("flexInt64(%s) = %d, want %d", in, int64(f), want)
		}
	}
	var f flexInt64
	if err := json.Unmarshal([]byte(`"nope"`), &f); err == nil {
		t.Error("expected error for non-numeric string")
	}
}

func TestParseKeyValues(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().StringSlice("config-field", []string{"region=us1", "env=prod"}, "")
	got, err := parseKeyValues(cmd, "config-field")
	if err != nil {
		t.Fatalf("parseKeyValues: %v", err)
	}
	want := map[string]string{"region": "us1", "env": "prod"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	bad := &cobra.Command{}
	bad.Flags().StringSlice("config-field", []string{"noequals"}, "")
	if _, err := parseKeyValues(bad, "config-field"); err == nil {
		t.Error("expected error for missing '='")
	}
}

// newServerFlagCmd builds a command carrying the full register/credentials flag
// set so the body-builder helpers can be exercised without the CLI wiring.
func newServerFlagCmd() *cobra.Command {
	cmd := &cobra.Command{}
	f := cmd.Flags()
	f.String("app-id", "", "")
	f.String("type", "", "")
	f.String("display-name", "", "")
	f.String("description", "", "")
	f.String("data-sensitivity", "", "")
	f.String("tool-prefix", "", "")
	f.StringSlice("user-id", nil, "")
	f.String("catalog-id", "", "")
	f.String("source-app-id", "", "")
	f.StringSlice("config-field", nil, "")
	f.String("hosted-config-file", "", "")
	f.String("url", "", "")
	f.String("transport", "", "")
	f.String("external-config-file", "", "")
	addAuthFlags(cmd)
	return cmd
}

func TestAuthArmFromFlags(t *testing.T) {
	cmd := newServerFlagCmd()
	_ = cmd.Flags().Set("auth", "bearer-token")
	_ = cmd.Flags().Set("bearer-token", "secret-xyz")
	arm, err := authArmFromFlags(cmd)
	if err != nil {
		t.Fatalf("authArmFromFlags: %v", err)
	}
	bt, ok := arm["bearerToken"].(map[string]any)
	if !ok || bt["token"] != "secret-xyz" {
		t.Errorf("bearerToken arm = %v", arm)
	}

	// custom-header
	cmd = newServerFlagCmd()
	_ = cmd.Flags().Set("auth", "custom-header")
	_ = cmd.Flags().Set("header-name", "X-Key")
	_ = cmd.Flags().Set("header-value", "v")
	arm, _ = authArmFromFlags(cmd)
	ch := arm["customHeader"].(map[string]any)
	if ch["headerName"] != "X-Key" || ch["headerValue"] != "v" {
		t.Errorf("customHeader arm = %v", arm)
	}

	// unset -> nil
	cmd = newServerFlagCmd()
	if arm, err := authArmFromFlags(cmd); err != nil || arm != nil {
		t.Errorf("unset auth = %v, %v; want nil, nil", arm, err)
	}

	// unsupported -> error (oauth2 must go via config-file)
	cmd = newServerFlagCmd()
	_ = cmd.Flags().Set("auth", "oauth2")
	if _, err := authArmFromFlags(cmd); err == nil {
		t.Error("expected error for --auth oauth2")
	}
}

func TestBuildRegisterBodyHosted(t *testing.T) {
	cmd := newServerFlagCmd()
	_ = cmd.Flags().Set("app-id", "app123")
	_ = cmd.Flags().Set("type", "hosted")
	_ = cmd.Flags().Set("display-name", "Datadog")
	_ = cmd.Flags().Set("catalog-id", "cat123")
	_ = cmd.Flags().Set("auth", "bearer-token")
	_ = cmd.Flags().Set("bearer-token", "tok")
	_ = cmd.Flags().Set("token-sharing", "per-user")
	_ = cmd.Flags().Set("config-field", "region=us1")
	_ = cmd.Flags().Set("require-tool-approval", "true")

	body, err := buildRegisterBody(cmd)
	if err != nil {
		t.Fatalf("buildRegisterBody: %v", err)
	}
	if body["serverType"] != "MCP_SERVER_TYPE_HOSTED" || body["appId"] != "app123" {
		t.Errorf("top-level = %v", body)
	}
	hc := body["hostedConfig"].(map[string]any)
	if hc["mcpServerCatalogId"] != "cat123" {
		t.Errorf("catalogId = %v", hc["mcpServerCatalogId"])
	}
	if hc["tokenSharing"] != "MCP_SERVER_TOKEN_SHARING_PER_USER" {
		t.Errorf("tokenSharing = %v", hc["tokenSharing"])
	}
	if hc["requireToolApproval"] != "OPTIONAL_BOOL_TRUE" {
		t.Errorf("requireToolApproval = %v", hc["requireToolApproval"])
	}
	if bt := hc["bearerToken"].(map[string]any); bt["token"] != "tok" {
		t.Errorf("bearerToken = %v", hc["bearerToken"])
	}
	cf := hc["configFields"].(map[string]string)
	if cf["region"] != "us1" {
		t.Errorf("configFields = %v", cf)
	}
}

// TestBuildRegisterBodyHostedRequiresCatalog pins the friendly guard when
// neither --catalog-id nor --hosted-config-file is supplied.
func TestBuildRegisterBodyHostedRequiresCatalog(t *testing.T) {
	cmd := newServerFlagCmd()
	_ = cmd.Flags().Set("app-id", "app123")
	_ = cmd.Flags().Set("type", "hosted")
	_ = cmd.Flags().Set("display-name", "X")
	if _, err := buildRegisterBody(cmd); err == nil {
		t.Error("expected error when --catalog-id missing for hosted")
	}
}

func TestBuildRegisterBodyExternal(t *testing.T) {
	cmd := newServerFlagCmd()
	_ = cmd.Flags().Set("app-id", "app123")
	_ = cmd.Flags().Set("type", "external")
	_ = cmd.Flags().Set("display-name", "My MCP")
	_ = cmd.Flags().Set("url", "https://mcp.example.com/sse")
	_ = cmd.Flags().Set("transport", "sse")
	_ = cmd.Flags().Set("auth", "none")

	body, err := buildRegisterBody(cmd)
	if err != nil {
		t.Fatalf("buildRegisterBody: %v", err)
	}
	ec := body["externalConfig"].(map[string]any)
	if ec["url"] != "https://mcp.example.com/sse" {
		t.Errorf("url = %v", ec["url"])
	}
	if ec["transportType"] != "MCP_SERVER_TRANSPORT_TYPE_SSE" {
		t.Errorf("transportType = %v", ec["transportType"])
	}
	if _, ok := ec["none"].(map[string]any); !ok {
		t.Errorf("expected none auth arm, got %v", ec)
	}
}

// TestBuildRegisterBodyExternalRequiresURL pins the friendly guard for external.
func TestBuildRegisterBodyExternalRequiresURL(t *testing.T) {
	cmd := newServerFlagCmd()
	_ = cmd.Flags().Set("app-id", "app123")
	_ = cmd.Flags().Set("type", "external")
	_ = cmd.Flags().Set("display-name", "X")
	if _, err := buildRegisterBody(cmd); err == nil {
		t.Error("expected error when --url missing for external")
	}
}

// TestBuildHostedConfigFileExclusive pins that mixing --hosted-config-file with
// convenience flags is rejected — including the auth-value/token-sharing flags
// that would otherwise be silently dropped alongside the file.
func TestBuildHostedConfigFileExclusive(t *testing.T) {
	for _, flag := range []struct{ name, val string }{
		{"catalog-id", "cat1"},
		{"bearer-token", "SECRET"},
		{"token-sharing", "per-user"},
		{"require-tool-approval", "true"},
	} {
		cmd := newServerFlagCmd()
		_ = cmd.Flags().Set("hosted-config-file", "cfg.json")
		_ = cmd.Flags().Set(flag.name, flag.val)
		if _, err := buildHostedConfig(cmd); err == nil {
			t.Errorf("expected mutual-exclusion error when --hosted-config-file combined with --%s", flag.name)
		}
	}
}

// TestBuildExternalConfigFileExclusive pins the same for external configs.
func TestBuildExternalConfigFileExclusive(t *testing.T) {
	cmd := newServerFlagCmd()
	_ = cmd.Flags().Set("external-config-file", "cfg.json")
	_ = cmd.Flags().Set("bearer-token", "SECRET")
	if _, err := buildExternalConfig(cmd); err == nil {
		t.Error("expected mutual-exclusion error for --external-config-file + --bearer-token")
	}
}

func TestDeriveCredentialUpdateMask(t *testing.T) {
	got := deriveCredentialUpdateMask(map[string]any{
		"bearerToken":  map[string]any{"token": "x"},
		"tokenSharing": "MCP_SERVER_TOKEN_SHARING_SHARED",
		"configFields": map[string]string{"a": "b"},
	})
	if got != "bearerToken,configFields,tokenSharing" {
		t.Errorf("mask = %q, want bearerToken,configFields,tokenSharing (sorted proto paths)", got)
	}
	if got := deriveCredentialUpdateMask(map[string]any{}); got != "" {
		t.Errorf("empty cfg mask = %q, want empty", got)
	}
}

// TestUpdateCredentialsMaskFromFlags is the regression guard for the bug where
// update-credentials masked the "externalConfig"/"hostedConfig" wrapper — which
// the backend keys the mask on the auth oneof CASE name (e.g. "bearerToken"),
// so masking the wrapper matched nothing and silently dropped every credential
// change. The mask must be the auth path, not the wrapper.
func TestUpdateCredentialsMaskFromFlags(t *testing.T) {
	cmd := newServerFlagCmd()
	_ = cmd.Flags().Set("type", "external")
	_ = cmd.Flags().Set("auth", "bearer-token")
	_ = cmd.Flags().Set("bearer-token", "secret")
	cfg, err := buildExternalConfig(cmd)
	if err != nil {
		t.Fatalf("buildExternalConfig: %v", err)
	}
	if mask := deriveCredentialUpdateMask(cfg); mask != "bearerToken" {
		t.Errorf("mask = %q, want %q (NOT externalConfig)", mask, "bearerToken")
	}
}

// TestServerCountRowLastCalled pins that last_called_at appears only when the
// view carries it, so search rows don't show a misleading always-empty column.
func TestServerCountRowLastCalled(t *testing.T) {
	if _, ok := serverCountRow(serverView{}, 3)["last_called_at"]; ok {
		t.Error("last_called_at should be absent when unset")
	}
	row := serverCountRow(serverView{LastCalledAt: "2026-07-14T00:00:00Z"}, 3)
	if row["last_called_at"] != "2026-07-14T00:00:00Z" {
		t.Errorf("last_called_at = %q, want the timestamp", row["last_called_at"])
	}
}

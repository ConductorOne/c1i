package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestAuthArmTemplate pins the oneof arm key and the exact field names emitted
// for each --auth mode. These must match what `register` sends on the wire (and
// what the api-reference documents) so a filled-in template round-trips.
func TestAuthArmTemplate(t *testing.T) {
	cases := []struct {
		mode    string
		armKey  string
		fields  []string
		wantErr bool
	}{
		{mode: "", armKey: "none", fields: nil},
		{mode: "none", armKey: "none", fields: nil},
		{mode: "bearer-token", armKey: "bearerToken", fields: []string{"token"}},
		{mode: "custom-header", armKey: "customHeader", fields: []string{"headerName", "headerValue"}},
		{mode: "basic-auth", armKey: "basicAuth", fields: []string{"username", "password"}},
		{mode: "oauth2", armKey: "oauth2", fields: []string{"mode", "clientId", "clientSecret", "authorizeUrl", "tokenUrl", "scopes"}},
		{mode: "aws-sigv4", armKey: "awsSigv4", fields: []string{"accessKeyId", "secretAccessKey", "sessionToken"}},
		{mode: "google-service-account", armKey: "googleServiceAccount", fields: []string{"credentialsJson", "scopes"}},
		{mode: "bogus", wantErr: true},
	}
	for _, c := range cases {
		arm, err := authArmTemplate(c.mode)
		if c.wantErr {
			if err == nil {
				t.Errorf("authArmTemplate(%q): expected error", c.mode)
			}
			continue
		}
		if err != nil {
			t.Fatalf("authArmTemplate(%q): %v", c.mode, err)
		}
		inner, ok := arm[c.armKey].(map[string]any)
		if !ok {
			t.Errorf("authArmTemplate(%q): missing arm key %q, got %v", c.mode, c.armKey, arm)
			continue
		}
		for _, f := range c.fields {
			if _, ok := inner[f]; !ok {
				t.Errorf("authArmTemplate(%q): arm missing field %q", c.mode, f)
			}
		}
	}
}

// TestAuthArmTemplateOAuth2ModeEnum pins the fully-prefixed enum value, matching
// the wire format the rest of the CLI uses (see mapTokenSharing et al.).
func TestAuthArmTemplateOAuth2ModeEnum(t *testing.T) {
	arm, err := authArmTemplate("oauth2")
	if err != nil {
		t.Fatal(err)
	}
	mode := arm["oauth2"].(map[string]any)["mode"]
	if mode != "MCP_SERVER_AUTH_OAUTH2_MODE_SERVICE" {
		t.Errorf("oauth2 mode = %v, want MCP_SERVER_AUTH_OAUTH2_MODE_SERVICE", mode)
	}
}

func newTemplateCmd() *cobra.Command {
	cmd := &cobra.Command{}
	f := cmd.Flags()
	f.String("type", "", "")
	f.String("auth", "", "")
	return cmd
}

// TestPrintConfigTemplateHosted checks the hosted template is valid JSON on
// stdout, carries the catalog-id + tokenSharing scaffolding, and keeps guidance
// on stderr (so `... 2>/dev/null > cfg.json` yields a clean config).
func TestPrintConfigTemplateHosted(t *testing.T) {
	cmd := newTemplateCmd()
	_ = cmd.Flags().Set("auth", "oauth2")
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	if err := printConfigTemplate(cmd); err != nil {
		t.Fatalf("printConfigTemplate: %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(out.Bytes(), &cfg); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, out.String())
	}
	if _, ok := cfg["mcpServerCatalogId"]; !ok {
		t.Error("hosted template missing mcpServerCatalogId")
	}
	if cfg["tokenSharing"] != "MCP_SERVER_TOKEN_SHARING_SHARED" {
		t.Errorf("tokenSharing = %v", cfg["tokenSharing"])
	}
	if _, ok := cfg["oauth2"].(map[string]any); !ok {
		t.Errorf("missing oauth2 arm: %v", cfg)
	}
	if !strings.Contains(errOut.String(), "config template") {
		t.Errorf("expected guidance on stderr, got %q", errOut.String())
	}
	// Guidance must not leak into stdout (would break JSON piping).
	if strings.Contains(out.String(), "#") {
		t.Errorf("stdout contains comment/guidance: %q", out.String())
	}
}

// TestPrintConfigTemplateExternal checks the external shape uses url/transport
// instead of the hosted-only fields.
func TestPrintConfigTemplateExternal(t *testing.T) {
	cmd := newTemplateCmd()
	_ = cmd.Flags().Set("type", "external")
	_ = cmd.Flags().Set("auth", "bearer-token")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})

	if err := printConfigTemplate(cmd); err != nil {
		t.Fatalf("printConfigTemplate: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(out.Bytes(), &cfg); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
	if _, ok := cfg["url"]; !ok {
		t.Error("external template missing url")
	}
	if _, ok := cfg["mcpServerCatalogId"]; ok {
		t.Error("external template should not carry mcpServerCatalogId")
	}
	if _, ok := cfg["bearerToken"].(map[string]any); !ok {
		t.Errorf("missing bearerToken arm: %v", cfg)
	}
}

// TestPrintConfigTemplateNoHTMLEscape guards that <placeholders> stay literal
// (json.Encoder HTML-escaping is off) so the output is human/agent-editable.
func TestPrintConfigTemplateNoHTMLEscape(t *testing.T) {
	cmd := newTemplateCmd()
	_ = cmd.Flags().Set("auth", "oauth2")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	if err := printConfigTemplate(cmd); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\\u003c") {
		t.Errorf("output HTML-escaped angle brackets to \\u003c: %s", out.String())
	}
	if !strings.Contains(out.String(), "<oauth-client-id>") {
		t.Errorf("expected literal <oauth-client-id> placeholder: %s", out.String())
	}
}

// TestTemplateRoundTripsThroughRegister proves the doc-comment guarantee: a
// template emitted by printConfigTemplate feeds back through the real register
// body builder (--hosted-config-file / --external-config-file) unchanged, with
// the enum strings intact. Because the config-file path is copied verbatim
// (no re-mapping), a future typo in an emitted enum value — e.g. transportType
// or tokenSharing — would break register at runtime; this test catches that at
// build time.
func TestTemplateRoundTripsThroughRegister(t *testing.T) {
	cases := []struct {
		serverType, auth, cfgKey, fileFlag string
		wantEnums                          map[string]string // top-level cfg key -> expected value
		wantArm                            string            // oneof arm key expected present
	}{
		{
			serverType: "hosted", auth: "oauth2",
			cfgKey: "hostedConfig", fileFlag: "hosted-config-file",
			wantEnums: map[string]string{"tokenSharing": "MCP_SERVER_TOKEN_SHARING_SHARED"},
			wantArm:   "oauth2",
		},
		{
			serverType: "external", auth: "bearer-token",
			cfgKey: "externalConfig", fileFlag: "external-config-file",
			wantEnums: map[string]string{"transportType": "MCP_SERVER_TRANSPORT_TYPE_STREAMABLE_HTTP"},
			wantArm:   "bearerToken",
		},
	}
	for _, c := range cases {
		// 1. Generate the template.
		gen := newTemplateCmd()
		_ = gen.Flags().Set("type", c.serverType)
		_ = gen.Flags().Set("auth", c.auth)
		var tmpl bytes.Buffer
		gen.SetOut(&tmpl)
		gen.SetErr(&bytes.Buffer{})
		if err := printConfigTemplate(gen); err != nil {
			t.Fatalf("%s/%s: generate: %v", c.serverType, c.auth, err)
		}

		// 2. Feed it back through the real register body builder via config-file.
		dir := t.TempDir()
		path := dir + "/config.json"
		if err := os.WriteFile(path, tmpl.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		reg := newServerFlagCmd()
		_ = reg.Flags().Set("app-id", "app1")
		_ = reg.Flags().Set("type", c.serverType)
		_ = reg.Flags().Set("display-name", "X")
		_ = reg.Flags().Set(c.fileFlag, path)
		body, err := buildRegisterBody(reg)
		if err != nil {
			t.Fatalf("%s/%s: buildRegisterBody: %v", c.serverType, c.auth, err)
		}

		// 3. The config the register would send must equal the template verbatim.
		got, ok := body[c.cfgKey].(map[string]any)
		if !ok {
			t.Fatalf("%s: body[%q] missing/not an object: %v", c.serverType, c.cfgKey, body[c.cfgKey])
		}
		var want map[string]any
		if err := json.Unmarshal(tmpl.Bytes(), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: register config does not round-trip.\n got=%v\nwant=%v", c.serverType, got, want)
		}
		// 4. Pin the specific enum strings and the auth arm.
		for k, v := range c.wantEnums {
			if got[k] != v {
				t.Errorf("%s: %s = %v, want %v", c.serverType, k, got[k], v)
			}
		}
		if _, ok := got[c.wantArm].(map[string]any); !ok {
			t.Errorf("%s: missing auth arm %q", c.serverType, c.wantArm)
		}
	}
}

// TestPrintConfigTemplateBadInput pins that unknown --auth and invalid --type
// error rather than emitting a silently wrong shape, and that both classify as
// usage errors (exit 2) so automation can distinguish misuse from real failures.
func TestPrintConfigTemplateBadInput(t *testing.T) {
	cases := []struct{ flag, val string }{
		{"auth", "bogus"},
		{"type", "sideways"},
	}
	for _, c := range cases {
		cmd := newTemplateCmd()
		_ = cmd.Flags().Set(c.flag, c.val)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		err := printConfigTemplate(cmd)
		if err == nil {
			t.Errorf("--%s %q: expected error", c.flag, c.val)
			continue
		}
		var usageErr *usageError
		if !errors.As(err, &usageErr) {
			t.Errorf("--%s %q: error %v is not a usageError (would exit 1, not 2)", c.flag, c.val, err)
		}
	}
}

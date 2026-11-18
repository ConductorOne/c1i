package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// updateWireCase is one update-command invocation. "{dir}" in an arg is
// replaced by a temp dir holding the *.json inputs below.
type updateWireCase struct {
	name string
	args []string
}

const updateWireGolden = "testdata/update_wire.golden.json"

// updateWireFiles are the --*-file inputs the cases reference.
var updateWireFiles = map[string]string{
	"mapper.json": testMapperJSON,
	"steps.json":  `[{"approval":{"users":{"userIds":["u1"]}}}]`,
	"rules.json":  `[{"condition":"true","policyKey":"k"}]`,
	"policy.json": `{"displayName":"n"}`,
	"auto.json":   `{"description":"d"}`,
	"ext.json":    `{"url":"https://mcp.example.invalid"}`,
	"hosted.json": `{"mcpServerCatalogId":"cat-9"}`,
	"empty.json":  `[]`,
	"badmap.json": `{"mappingCases":[]}`,
}

func updateWireCases() []updateWireCase {
	var cases []updateWireCase
	add := func(cmd string, base []string, variants ...[]string) {
		for _, v := range variants {
			args := append(strings.Fields(cmd), base...)
			args = append(args, v...)
			cases = append(cases, updateWireCase{name: cmd + " " + strings.Join(v, " "), args: args})
		}
	}
	none := []string{}
	add("apps update", []string{"app-1"},
		none,
		[]string{"--display-name", "N"},
		[]string{"--description", "D"},
		[]string{"--description", ""},
		[]string{"--grant-policy-id", "gp-1"},
		[]string{"--revoke-policy-id", "rp-1"},
		[]string{"--certify-policy-id", "cp-1"},
		[]string{"--unbind-policy", "grant"},
		[]string{"--unbind-policy", "grant", "--unbind-policy", "certify"},
		[]string{"--grant-policy-id", "gp-1", "--unbind-policy", "grant"},
		[]string{"--identity-matching", "strict"},
		[]string{"--identity-matching", "custom"},
		[]string{"--identity-matching", "bogus"},
		[]string{"--app-user-mapper-file", "{dir}/mapper.json"},
		[]string{"--description", "D", "--display-name", "N"},
		[]string{"--identity-matching", "display-name", "--revoke-policy-id", "rp-1", "--description", ""},
		[]string{"--app-user-mapper-file", "{dir}/mapper.json", "--identity-matching", "strict", "--certify-policy-id", "cp-1", "--grant-policy-id", "gp-1", "--display-name", "N", "--description", "D"},
	)
	add("entitlements update", []string{"ent-1", "--app-id", "a1"},
		none,
		[]string{"--display-name", "N"},
		[]string{"--description", "D"},
		[]string{"--description", ""},
		[]string{"--grant-policy-id", "gp-1"},
		[]string{"--revoke-policy-id", "rp-1"},
		[]string{"--certify-policy-id", "cp-1"},
		[]string{"--emergency-grant-policy-id", "gp-e"},
		[]string{"--unbind-policy", "emergency-grant"},
		[]string{"--emergency-grant-enabled", "--emergency-grant-policy-id", "gp-e"},
		[]string{"--emergency-grant-enabled=false"},
		[]string{"--emergency-grant-enabled=false", "--unbind-policy", "emergency-grant"},
		[]string{"--emergency-grant-enabled"},
		[]string{"--duration-grant", "8h"},
		[]string{"--duration-grant", "1h30m"},
		[]string{"--duration-grant", "-1h"},
		[]string{"--duration-unset"},
		[]string{"--duration-unset=false"},
		[]string{"--duration-grant", "8h", "--duration-unset"},
		[]string{"--duration-unset", "--description", "D", "--display-name", "N"},
		[]string{"--duration-grant", "3600s", "--emergency-grant-enabled=false", "--certify-policy-id", "cp-1", "--description", ""},
	)
	add("access-profiles update", []string{"ap-1"},
		none,
		[]string{"--display-name", "N"},
		[]string{"--description", "D"},
		[]string{"--description", ""},
		[]string{"--published"},
		[]string{"--published=false"},
		[]string{"--visible-to-everyone"},
		[]string{"--visible-to-everyone=false"},
		[]string{"--request-bundle"},
		[]string{"--enrollment-behavior", "bypass"},
		[]string{"--enrollment-behavior", ""},
		[]string{"--unenrollment-behavior", "revoke-all"},
		[]string{"--unenrollment-behavior", ""},
		[]string{"--unenrollment-entitlement-behavior", "enforce"},
		[]string{"--unenrollment-entitlement-behavior", ""},
		[]string{"--request-bundle=false", "--description", "D"},
		[]string{"--published", "--request-bundle", "--unenrollment-entitlement-behavior", "bypass", "--display-name", "N", "--enrollment-behavior", "enforce", "--visible-to-everyone=false", "--description", ""},
	)
	add("automations update", []string{"au-1"},
		none,
		[]string{"--display-name", "N"},
		[]string{"--description", "D"},
		[]string{"--description", ""},
		[]string{"--enabled"},
		[]string{"--enabled=false"},
		[]string{"--enabled", "--display-name", "N"},
		[]string{"--description", "", "--enabled=false", "--display-name", "N"},
		[]string{"--body-file", "{dir}/auto.json", "--update-mask", "description"},
		[]string{"--body-file", "{dir}/auto.json"},
		[]string{"--body-file", "{dir}/auto.json", "--update-mask", "description", "--enabled"},
	)
	add("access-reviews templates update", []string{"t-1"},
		none,
		[]string{"--display-name", "N"},
		[]string{"--description", "D"},
		[]string{"--description", ""},
		[]string{"--review-instructions", ""},
		[]string{"--policy-id", "cp-1"},
		[]string{"--policy-id", "gp-1"},
		[]string{"--policy-id", "pol-404"},
		[]string{"--scope-type", "by-users"},
		[]string{"--scope-type", "bogus"},
		[]string{"--description", "D", "--display-name", "N", "--scope-type", "by-entitlements"},
		[]string{"--body-file", "{dir}/auto.json", "--update-mask", "description"},
		[]string{"--body-file", "{dir}/auto.json"},
		[]string{"--body-file", "{dir}/auto.json", "--update-mask", "description", "--display-name", "N"},
	)
	add("policies update", []string{"pol-1"},
		none,
		[]string{"--display-name", "N"},
		[]string{"--description", "D"},
		[]string{"--description", ""},
		[]string{"--policy-type", "grant"},
		[]string{"--policy-type", "revoke", "--steps-file", "{dir}/steps.json"},
		[]string{"--steps-file", "{dir}/steps.json"},
		[]string{"--rules-file", "{dir}/rules.json"},
		[]string{"--description", "D", "--display-name", "N"},
		[]string{"--rules-file", "{dir}/rules.json", "--steps-file", "{dir}/steps.json", "--policy-type", "grant", "--description", "", "--display-name", "N"},
		[]string{"--body-file", "{dir}/policy.json", "--update-mask", "displayName"},
		[]string{"--body-file", "{dir}/policy.json", "--display-name", "N"},
	)
	add("mcp servers update", []string{"cn-1", "--app-id", "a1"},
		none,
		[]string{"--display-name", "N"},
		[]string{"--description", "D"},
		[]string{"--description", ""},
		[]string{"--data-sensitivity", "internal"},
		[]string{"--data-sensitivity", ""},
		[]string{"--data-sensitivity", "bogus"},
		[]string{"--tool-prefix", "p"},
		[]string{"--tool-prefix", ""},
		[]string{"--require-tool-approval"},
		[]string{"--require-tool-approval=false"},
		[]string{"--tool-prefix", "p", "--description", "D"},
		[]string{"--require-tool-approval=false", "--tool-prefix", "", "--data-sensitivity", "public", "--description", "", "--display-name", "N"},
	)
	add("mcp servers update-credentials", []string{"cn-1", "--app-id", "a1"},
		[]string{"--type", "external"},
		[]string{"--type", "external", "--server-url", "https://mcp.example.invalid"},
		[]string{"--type", "external", "--transport", "streamable-http"},
		[]string{"--type", "external", "--auth", "bearer-token", "--bearer-token", "T"},
		[]string{"--type", "external", "--auth", "none"},
		[]string{"--type", "external", "--auth", "custom-header", "--header-name", "H", "--header-value", "V"},
		[]string{"--type", "external", "--auth", "basic-auth", "--basic-auth-username", "U", "--basic-auth-password", "P"},
		[]string{"--type", "external", "--require-tool-approval"},
		[]string{"--type", "external", "--require-tool-approval=false", "--server-url", "https://mcp.example.invalid"},
		[]string{"--type", "external", "--update-mask", "url", "--server-url", "https://mcp.example.invalid"},
		[]string{"--type", "external", "--update-mask", "url"},
		[]string{"--type", "external", "--server-url", "https://mcp.example.invalid", "--auth", "bearer-token", "--bearer-token", "T", "--token-sharing", "per-user"},
		[]string{"--type", "hosted", "--catalog-id", "cat-9", "--auth", "none"},
		[]string{"--type", "hosted", "--auth", "bearer-token", "--bearer-token", "T"},
		[]string{"--type", "hosted", "--config-field", "k=v"},
		[]string{"--type", "hosted", "--config-field", "k=v", "--source-app-id", "s1", "--catalog-id", "cat-9"},
	)
	add("mcp toolsets update", []string{"ts-1", "--app-id", "a1", "--connector-id", "cn-1"},
		none,
		[]string{"--display-name", "N"},
		[]string{"--description", "D"},
		[]string{"--description", ""},
		[]string{"--description", "D", "--display-name", "N"},
	)
	add("service-principals update", []string{"sp-1"},
		none,
		[]string{"--display-name", "N"},
	)
	add("service-principals credentials update", []string{"cr-1", "--service-principal-id", "sp-1"},
		none,
		[]string{"--display-name", "N"},
	)
	add("webhooks update", []string{"wh-1"},
		none,
		[]string{"--display-name", "Name"},
		[]string{"--destination-url", "https://hooks.example.invalid/x"},
		[]string{"--destination-url", "http://hooks.example.invalid/x"},
		[]string{"--description", "D"},
		[]string{"--description", ""},
		[]string{"--callback-timeout", "10m"},
		[]string{"--callback-timeout", "-1h"},
		[]string{"--callback-timeout", "600s", "--description", "D", "--destination-url", "https://hooks.example.invalid/x", "--display-name", "Name"},
		[]string{"--description", "", "--callback-timeout", "0s"},
	)
	add("apps update", []string{"app-1"},
		[]string{"--unbind-policy", "revoke"},
		[]string{"--app-user-mapper-file", "{dir}/badmap.json"},
	)
	add("entitlements update", []string{"ent-1", "--app-id", "a1"},
		[]string{"--unbind-policy", "grant"},
		[]string{"--unbind-policy", "revoke"},
		[]string{"--unbind-policy", "certify"},
	)
	add("policies update", []string{"pol-1"},
		[]string{"--policy-type", "grant", "--steps-file", "{dir}/empty.json"},
		[]string{"--policy-type", "grant", "--steps-file", "{dir}/empty.json", "--allow-deny-all"},
	)
	add("policies update", []string{"pol-404"},
		[]string{"--steps-file", "{dir}/steps.json", "--description", "lookup fails"},
	)
	add("mcp servers update-credentials", []string{"cn-1", "--app-id", "a1"},
		[]string{"--type", "external", "--external-config-file", "{dir}/ext.json"},
		[]string{"--type", "hosted", "--hosted-config-file", "{dir}/hosted.json"},
		[]string{"--type", "hosted"},
		[]string{"--type", "hosted", "--auth", "none", "--update-mask", "none"},
		[]string{"--type", "external", "--token-sharing", "per-user"},
		[]string{"--type", "external", "--transport", "bogus"},
	)
	add("webhooks create", nil,
		none,
		[]string{"--display-name", "Name", "--destination-url", "https://hooks.example.invalid/x"},
		[]string{"--display-name", "Name", "--destination-url", "https://hooks.example.invalid/x", "--description", "", "--callback-timeout", "10m"},
		[]string{"--display-name", "Name", "--destination-url", "http://hooks.example.invalid/x"},
		[]string{"--display-name", "Name", "--destination-url", "https://hooks.example.invalid/x", "--callback-timeout", "-1h"},
	)
	add("access-profiles create", nil,
		none,
		[]string{"--display-name", "N"},
		[]string{"--display-name", "N", "--description", ""},
		[]string{"--display-name", "N", "--published", "--visible-to-everyone=false", "--request-bundle"},
		[]string{"--display-name", "N", "--enrollment-behavior", "bypass", "--unenrollment-behavior", "revoke-all", "--unenrollment-entitlement-behavior", "enforce"},
		[]string{"--display-name", "N", "--enrollment-behavior", ""},
		[]string{"--display-name", "N", "--enrollment-behavior", "bogus"},
	)
	add("settings ai-governance update", nil,
		none,
		[]string{"--default-tool-classification", "read", "--audit-verbosity", "full", "--auto-discovery=false", "--discovery-interval", "1h"},
		[]string{"--prefer-code-mode", "--surface-requestable-tools=false", "--untrusted-judge-disable=false"},
		[]string{"--allowed-client-type", "personal", "--allowed-client-type", "MCP_CLIENT_TYPE_SERVICE"},
		[]string{"--allowed-client-type", "bogus"},
		[]string{"--inactivity-hide-after", "24h", "--retention-delete-after", "2160h"},
		[]string{"--inactivity-close-after", "1.5h"},
		[]string{"--body-file", "{dir}/auto.json", "--update-mask", "codeModeConcurrency"},
		[]string{"--body-file", "{dir}/auto.json", "--audit-verbosity", "full", "--update-mask", "auditVerbosity"},
	)
	add("settings session update", nil,
		none,
		[]string{"--max-session-length", "8h", "--idle-timeout", "1800s", "--enable-idle-timeout"},
		[]string{"--external-clients-enabled=false", "--client-id-metadata-document-policy", "allowlist-only"},
	)
	add("settings sso update", nil,
		none,
		[]string{"--enabled", "--default-subject-type", "pairwise", "--default-assertion-lifetime", "5m", "--default-id-token-alg", "es256"},
	)
	add("settings cross-app-access update", nil,
		none,
		[]string{"--enabled=false", "--default-grant-lifetime", "1h", "--allow-refresh-token-subjects", "--xaa-id-token-lifetime", "300s"},
		[]string{"--default-signing-algorithm", "eddsa", "--enabled-signing-algorithm", "eddsa", "--enabled-signing-algorithm", "rs256"},
	)
	add("settings contacts update", nil,
		none,
		[]string{"--security-email", "a@example.com", "--security-email", "b@example.com", "--operations-email", "ops@example.com"},
		[]string{"--billing-email", ""},
	)
	add("settings request update", nil,
		none,
		[]string{"--skip-justification", "--max-bulk-entitlement-selection", "0"},
		[]string{"--max-bulk-entitlement-selection", "x"},
	)
	add("apps routing-rules update", []string{"rr-1", "--app-id", "app-1"},
		none,
		[]string{"--display-name", "N"},
		[]string{"--enabled=false"},
		[]string{"--condition", ""},
		[]string{"--description", ""},
		[]string{"--request-policy-id", "gp-1"},
		[]string{"--emergency-grant-policy-id", "gp-2"},
		[]string{"--emergency-grant-enabled=false"},
		[]string{"--max-grant-duration", "8h"},
		[]string{"--request-schema-id", "rs-1"},
	)
	return cases
}

type updateWireResult struct {
	Exit   int    `json:"exit"`
	Method string `json:"method,omitempty"`
	Path   string `json:"path,omitempty"`
	Body   any    `json:"body,omitempty"`
}

func runUpdateWireCase(t *testing.T, dir string, c updateWireCase) updateWireResult {
	t.Helper()
	t.Setenv("C1I_URL", mutTestURL)
	clearViperOverrides(t, "dry_run")
	s := newMutServer(t, func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			p := r.URL.Path
			switch {
			case p == "/api/v1/policies/pol-404":
				return mutReply{status: http.StatusNotFound, body: `{"code":5,"message":"not found"}`}
			case strings.HasPrefix(p, "/api/v1/policies/"):
				typ := "POLICY_TYPE_GRANT"
				switch id := strings.TrimPrefix(p, "/api/v1/policies/"); {
				case strings.HasPrefix(id, "rp"):
					typ = "POLICY_TYPE_REVOKE"
				case strings.HasPrefix(id, "cp"):
					typ = "POLICY_TYPE_CERTIFY"
				}
				return mutReply{body: `{"policy":{"policyType":"` + typ + `"}}`}
			case strings.HasPrefix(p, "/api/v1/access_review_template/"):
				return mutReply{body: `{"accessReviewTemplate":{"id":"t-1","displayName":"cur","policyId":"cp-0"}}`}
			case p == "/api/v1/apps/app-1":
				return mutReply{body: `{"app":{"id":"app-1"}}`}
			case strings.Contains(p, "/entitlement_configuration_rules/"):
				return mutReply{body: `{"routingRule":{"id":"rr-1","appId":"app-1","displayName":"cur","description":"desc","priority":2,"enabled":true,"condition":"c == 1",` +
					`"settings":{"requestPolicyId":"gp-0","emergencyGrantPolicyId":"gp-9","emergencyGrantEnabled":true,"maxGrantDuration":"3600s","requestSchemaId":"rs-0"},"deletedAt":null}}`}
			case strings.Contains(p, "/mcp_servers/"):
				return mutReply{body: `{"mcpServer":{"mcpServerCatalogId":"cat-1"}}`}
			}
		}
		return mutReply{body: `{}`}
	})
	args := make([]string, len(c.args))
	for i, a := range c.args {
		args[i] = strings.ReplaceAll(a, "{dir}", dir)
	}
	r := runMutationCLI(t, s.srv, 0, nil, args...)
	out := updateWireResult{Exit: r.exit}
	for _, q := range s.requests() {
		if q.method == http.MethodGet {
			continue
		}
		out.Method, out.Path = q.method, q.rawPath
		if q.body != "" {
			if err := json.Unmarshal([]byte(q.body), &out.Body); err != nil {
				t.Fatalf("%s: body %q: %v", c.name, q.body, err)
			}
		}
	}
	return out
}

// TestUpdateCommandsWireIsPinned pins the request every update command sends
// (method, path, body incl. updateMask) and its exit code across a flag
// matrix, so the shared update builder can't change a byte on the wire. Set
// UPDATE_WIRE_GOLDEN=1 to rewrite the file (review the diff).
func TestUpdateCommandsWireIsPinned(t *testing.T) {
	dir := t.TempDir()
	for n, content := range updateWireFiles {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]updateWireResult{}
	for _, c := range updateWireCases() {
		if _, dup := got[c.name]; dup {
			t.Fatalf("duplicate case %q", c.name)
		}
		got[c.name] = runUpdateWireCase(t, dir, c)
	}
	if os.Getenv("UPDATE_WIRE_GOLDEN") != "" {
		b, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(updateWireGolden, append(b, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(updateWireGolden)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]updateWireResult
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	var names []string
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, ok := want[n]
		if !ok {
			t.Errorf("%q: not in %s", n, updateWireGolden)
			continue
		}
		gj, _ := json.Marshal(got[n])
		wj, _ := json.Marshal(w)
		if string(gj) != string(wj) {
			t.Errorf("%q:\n got %s\nwant %s", n, gj, wj)
		}
	}
	for n := range want {
		if _, ok := got[n]; !ok {
			t.Errorf("%s lists %q, which is no longer a case", updateWireGolden, n)
		}
	}
}

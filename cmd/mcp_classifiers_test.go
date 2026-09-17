package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func writeClassifierJSON(t *testing.T, name, content string) string {
	t.Helper()
	path := t.TempDir() + "/" + name
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

func enableClassifierDryRun(t *testing.T) {
	t.Helper()
	original := viper.GetBool("dry_run")
	viper.Set("dry_run", true)
	t.Cleanup(func() { viper.Set("dry_run", original) })
}

func setClassifierFlag(t *testing.T, cmd *cobra.Command, name, value string) {
	t.Helper()
	if err := cmd.Flags().Set(name, value); err != nil {
		t.Fatalf("setting --%s=%s: %v", name, value, err)
	}
}

func TestClassifierMutationsDryRunUsePublicRequestShapes(t *testing.T) {
	classifierFile := writeClassifierJSON(t, "classifier.json", `{"displayName":"Observe MCP"}`)
	bindingFile := writeClassifierJSON(t, "binding.json", `{"surface":"CLASSIFIER_TYPE_GATEWAY","classifierId":"classifier-1"}`)
	templateFile := writeClassifierJSON(t, "template.json", `{"templateVersion":"v2","params":{"grant_policy_id":"policy-1"}}`)
	policyFile := writeClassifierJSON(t, "policy.json", `{"defaultOutcome":"AGENT_CLASSIFIER_RULE_OUTCOME_ALLOWED"}`)
	toolGateFile := writeClassifierJSON(t, "tool-gate.json", `{"displayName":"Destructive","grantPolicyId":"policy-1","filter":{"builtInPattern":"TOOL_GATE_BUILT_IN_PATTERN_DESTRUCTIVE_ACTION"}}`)

	cases := []struct {
		name     string
		cmd      *cobra.Command
		args     []string
		bodyFile string
		mask     string
		contains []string
		absent   []string
	}{
		{
			name: "classifier create", cmd: mcpClassifiersCreateCmd, bodyFile: classifierFile,
			contains: []string{"[dry-run] POST /api/v1/classifiers", `"classifier"`, `"displayName": "Observe MCP"`},
		},
		{
			name: "classifier update", cmd: mcpClassifiersUpdateCmd, args: []string{"classifier/1"}, bodyFile: classifierFile, mask: "description",
			contains: []string{"/api/v1/classifiers/classifier%2F1", `"id": "classifier/1"`, `"updateMask": "description"`},
		},
		{
			name: "binding create", cmd: mcpClassifierBindingsCreateCmd, bodyFile: bindingFile,
			contains: []string{"/api/v1/classifier_bindings", `"binding"`, `"classifierId": "classifier-1"`},
		},
		{
			name: "template instantiate", cmd: mcpClassifierTemplatesInstantiateCmd, args: []string{"template/1"}, bodyFile: templateFile,
			contains: []string{"/api/v1/classifier_templates/template%2F1/instantiate", `"templateVersion": "v2"`},
			absent:   []string{`"templateId"`},
		},
		{
			name: "template add rule", cmd: mcpClassifierTemplatesAddRuleCmd, args: []string{"classifier/1", "template/1"}, bodyFile: templateFile,
			contains: []string{"/api/v1/classifiers/classifier%2F1/rules/from_template", `"templateId": "template/1"`},
			absent:   []string{`"classifierId"`},
		},
		{
			name: "singleton policy update", cmd: mcpClassifierPolicyUpdateCmd, bodyFile: policyFile, mask: "defaultOutcome",
			contains: []string{"/api/v1/settings/ai-governance/agent-classifier-policy", `"policy"`, `"updateMask": "defaultOutcome"`},
		},
		{
			name: "tool gate create", cmd: mcpClassifierToolGatesCreateCmd, bodyFile: toolGateFile,
			contains: []string{"/api/v1/tool_gates", `"grantPolicyId": "policy-1"`},
			absent:   []string{`"toolGate"`},
		},
		{
			name: "tool gate update", cmd: mcpClassifierToolGatesUpdateCmd, args: []string{"gate/1"}, bodyFile: toolGateFile, mask: "enabled",
			contains: []string{"/api/v1/tool_gates/gate%2F1", `"toolGate"`, `"id": "gate/1"`, `"updateMask": "enabled"`},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resetCmdFlags(t, tt.cmd)
			enableClassifierDryRun(t)
			t.Setenv("C1I_URL", "https://example.invalid")
			if tt.bodyFile != "" {
				setClassifierFlag(t, tt.cmd, "body-file", tt.bodyFile)
			}
			if tt.mask != "" {
				setClassifierFlag(t, tt.cmd, "update-mask", tt.mask)
			}
			var out bytes.Buffer
			tt.cmd.SetOut(&out)
			t.Cleanup(func() { tt.cmd.SetOut(nil) })
			tt.cmd.SetContext(context.Background())
			if err := tt.cmd.RunE(tt.cmd, tt.args); err != nil {
				t.Fatalf("RunE: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output %q does not contain %q", out.String(), want)
				}
			}
			for _, unwanted := range tt.absent {
				if strings.Contains(out.String(), unwanted) {
					t.Errorf("output %q unexpectedly contains %q", out.String(), unwanted)
				}
			}
		})
	}
}

func TestClassifierUpdatesRequireExplicitMask(t *testing.T) {
	resetCmdFlags(t, mcpClassifiersUpdateCmd)
	setClassifierFlag(t, mcpClassifiersUpdateCmd, "body-file", writeClassifierJSON(t, "classifier.json", `{"description":"updated"}`))
	mcpClassifiersUpdateCmd.SetContext(context.Background())
	err := mcpClassifiersUpdateCmd.RunE(mcpClassifiersUpdateCmd, []string{"classifier-1"})
	if err == nil || !strings.Contains(err.Error(), "--update-mask is required") {
		t.Errorf("error = %v, want missing update mask", err)
	}
}

func TestClassifierUpdateRequiresDisplayName(t *testing.T) {
	resetCmdFlags(t, mcpClassifiersUpdateCmd)
	enableClassifierDryRun(t)
	setClassifierFlag(t, mcpClassifiersUpdateCmd, "body-file", writeClassifierJSON(t, "classifier.json", `{"description":"updated"}`))
	setClassifierFlag(t, mcpClassifiersUpdateCmd, "update-mask", "description")
	mcpClassifiersUpdateCmd.SetContext(context.Background())
	err := mcpClassifiersUpdateCmd.RunE(mcpClassifiersUpdateCmd, []string{"classifier-1"})
	if err == nil || !strings.Contains(err.Error(), "include displayName") {
		t.Errorf("error = %v, want missing displayName", err)
	}
}

func TestClassifierListPaginatesWithClassifierEnvelope(t *testing.T) {
	var tokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/classifiers" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		tokens = append(tokens, r.URL.Query().Get("page_token"))
		if r.URL.Query().Get("page_token") == "next" {
			_, _ = w.Write([]byte(`{"classifiers":[{"id":"classifier-2","displayName":"Second"}],"nextPageToken":""}`))
			return
		}
		_, _ = w.Write([]byte(`{"classifiers":[{"id":"classifier-1","displayName":"First"}],"nextPageToken":"next"}`))
	}))
	t.Cleanup(srv.Close)

	original := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = original })
	t.Setenv("C1I_URL", "https://example.invalid")
	resetCmdFlags(t, mcpClassifiersListCmd)
	var out bytes.Buffer
	mcpClassifiersListCmd.SetOut(&out)
	t.Cleanup(func() { mcpClassifiersListCmd.SetOut(nil) })
	mcpClassifiersListCmd.SetContext(context.Background())
	if err := mcpClassifiersListCmd.RunE(mcpClassifiersListCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if got, want := strings.Join(tokens, ","), ",next"; got != want {
		t.Errorf("page tokens = %q, want %q", got, want)
	}
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("unmarshaling row %q: %v", line, err)
		}
		rows = append(rows, row)
	}
	if len(rows) != 2 || rows[0]["id"] != "classifier-1" || rows[1]["id"] != "classifier-2" {
		t.Errorf("rows = %#v, want both classifier IDs in order", rows)
	}
}

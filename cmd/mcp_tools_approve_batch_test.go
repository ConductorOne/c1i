package cmd

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestMcpToolsApproveBatchesOnePostPerId pins C146: `approve` takes one or
// more tool ids and sends one request per id (the API has no batch approve).
// Before the fix, ExactArgs(1) rejected a second id outright. A preview is one
// document with one request per id, in argument order.
func TestMcpToolsApproveBatchesOnePostPerId(t *testing.T) {
	resetCmdFlags(t, rootCmd)
	// approve's own --app-id/--connector-id live on the shared command object;
	// resetCmdFlags also restores them on cleanup.
	resetCmds(t, mcpToolsApproveCmd)
	t.Setenv("C1I_URL", "")
	withDryRun(t)

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
	rootCmd.SetArgs([]string{
		"mcp", "tools", "approve", "tool-a", "tool-b", "tool-c",
		"--app-id", "app-x", "--connector-id", "conn-y",
		"--dry-run", "--url", "acme.conductor.one",
	})
	if err := rootCmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("approve of three ids errored: %v", err)
	}

	var doc struct {
		DryRun   bool `json:"dryRun"`
		Requests []struct {
			Method string `json:"method"`
			URL    string `json:"url"`
		} `json:"requests"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("preview is not one JSON document: %v\n%s", err, out.String())
	}
	ids := []string{"tool-a", "tool-b", "tool-c"}
	if !doc.DryRun || len(doc.Requests) != len(ids) {
		t.Fatalf("got %+v, want dryRun with %d requests (one per id)", doc, len(ids))
	}
	for i, id := range ids {
		want := "https://acme.conductor.one/api/v1/apps/app-x/connectors/conn-y/mcp_tools/" + id
		if doc.Requests[i].Method != "POST" || doc.Requests[i].URL != want {
			t.Errorf("request %d = %+v, want POST %s", i, doc.Requests[i], want)
		}
	}
}

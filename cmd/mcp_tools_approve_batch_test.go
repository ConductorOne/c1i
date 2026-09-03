package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// TestMcpToolsApproveBatchesOnePostPerId pins C146: `approve` takes one or
// more tool ids and sends one request per id (the API has no batch approve).
// Before the fix, ExactArgs(1) rejected a second id outright.
func TestMcpToolsApproveBatchesOnePostPerId(t *testing.T) {
	resetRootURLFlag(t)
	resetRootDryRunFlag(t)
	// approve's own --app-id/--connector-id live on the shared command object;
	// clear them before and after so this test neither inherits nor leaks a
	// Changed flag (the C182 order-dependence class).
	resetCmds(t, mcpToolsApproveCmd)
	t.Cleanup(func() { resetCmds(t, mcpToolsApproveCmd) })
	t.Setenv("C1I_URL", "")
	withDryRun(t)

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
	rootCmd.SetArgs([]string{
		"mcp", "tools", "approve", "tool-a", "tool-b", "tool-c",
		"--app-id", "app-x", "--connector-id", "conn-y",
		"--dry-run", "--url", "acme.conductor.one",
	})
	if err := rootCmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("approve of three ids errored: %v", err)
	}

	got := out.String()
	for _, id := range []string{"tool-a", "tool-b", "tool-c"} {
		want := "[dry-run] POST /api/v1/apps/app-x/connectors/conn-y/mcp_tools/" + id
		if !strings.Contains(got, want) {
			t.Errorf("missing preview for %s;\nwant substring %q\ngot:\n%s", id, want, got)
		}
	}
	if n := strings.Count(got, "[dry-run] POST"); n != 3 {
		t.Errorf("got %d POST previews, want 3 (one per id)", n)
	}
}

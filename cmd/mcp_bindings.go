package cmd

import "github.com/spf13/cobra"

var mcpBindingsCmd = &cobra.Command{
	Use:   "bindings",
	Short: "Inspect MCP toolset ↔ tool bindings",
	Long: `Inspect and manage bindings between MCP tools and MCP toolsets (access
profiles) — which tools are members of which toolset. Backed by
/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_toolsets/{toolset_id}/tool_bindings.

This is a different object from an entitlement "proxy binding"
(entitlement -> entitlement, e.g.
POST /api/v1/apps/{src_app}/{src_entitlement}/bindings/{dst_app}/{dst_entitlement}).
c1i has no dedicated command for entitlement proxy bindings; see
"c1i docs guide delegate-entitlement-provisioning" for a runbook, or use
"c1i api" directly.`,
}

func init() {
	mcpCmd.AddCommand(mcpBindingsCmd)
}

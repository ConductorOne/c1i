package cmd

import "github.com/spf13/cobra"

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage MCP servers, tools, and toolsets",
	Long: `Inspect and manage the MCP (Model Context Protocol) surface for a tenant.

MCP tools are discovered from a registered MCP server and approved by an admin
before they become callable through C1. Toolsets group tools into a single
AppEntitlement so they can be requested and granted as a unit.

Subcommands:
  servers   - Register, configure, and inspect MCP servers (+ catalog and
              per-user connections)
  tools     - List, search, get, approve, delete, and view history for tools
  toolsets  - CRUD toolsets, look up by entitlement, list user-requestable
              connectors
  bindings  - List, create, delete, reverse-lookup, and view history for the
              tool ↔ toolset bindings`,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}

package cmd

import "github.com/spf13/cobra"

var mcpToolsetsCmd = &cobra.Command{
	Use:   "toolsets",
	Short: "Manage MCP toolsets (admin-curated tool groupings)",
}

func init() {
	mcpCmd.AddCommand(mcpToolsetsCmd)
}

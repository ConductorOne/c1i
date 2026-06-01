package cmd

import "github.com/spf13/cobra"

var mcpBindingsCmd = &cobra.Command{
	Use:   "bindings",
	Short: "Inspect MCP toolset ↔ tool bindings",
}

func init() {
	mcpCmd.AddCommand(mcpBindingsCmd)
}

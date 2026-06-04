package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

var mcpToolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Manage MCP tools discovered from a server",
}

func init() {
	mcpCmd.AddCommand(mcpToolsCmd)
}

// mapToolState translates a user-friendly --state value to the API enum.
// Input is case-insensitive so `--state approved` and `APPROVED` both work.
func mapToolState(s string) string {
	switch strings.ToLower(s) {
	case "pending", "pending_review":
		return "MCP_TOOL_STATE_PENDING_REVIEW"
	case "approved":
		return "MCP_TOOL_STATE_APPROVED"
	case "disabled":
		return "MCP_TOOL_STATE_DISABLED"
	case "removed":
		return "MCP_TOOL_STATE_REMOVED"
	default:
		return s
	}
}

// mapToolClassification translates a user-friendly --classification value to
// the API enum. Input is case-insensitive.
func mapToolClassification(s string) string {
	switch strings.ToLower(s) {
	case "read":
		return "TOOL_CLASSIFICATION_READ"
	case "write":
		return "TOOL_CLASSIFICATION_WRITE"
	case "destructive":
		return "TOOL_CLASSIFICATION_DESTRUCTIVE"
	case "sensitive":
		return "TOOL_CLASSIFICATION_SENSITIVE"
	case "dangerous":
		return "TOOL_CLASSIFICATION_DANGEROUS"
	default:
		return s
	}
}

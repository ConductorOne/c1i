package cmd

import (
	"github.com/spf13/cobra"
)

var mcpToolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Manage MCP tools discovered from a server",
}

func init() {
	mcpCmd.AddCommand(mcpToolsCmd)
}

var toolStateFlag = enumFlag{flag: "state", prefix: "MCP_TOOL_STATE_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "pending", api: "MCP_TOOL_STATE_PENDING_REVIEW"},
	{name: "approved", api: "MCP_TOOL_STATE_APPROVED"},
	{name: "disabled", api: "MCP_TOOL_STATE_DISABLED"},
	{name: "removed", api: "MCP_TOOL_STATE_REMOVED"},
}}

var toolClassificationFlag = enumFlag{flag: "classification", prefix: "TOOL_CLASSIFICATION_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "read", api: "TOOL_CLASSIFICATION_READ"},
	{name: "write", api: "TOOL_CLASSIFICATION_WRITE"},
	{name: "destructive", api: "TOOL_CLASSIFICATION_DESTRUCTIVE"},
	{name: "sensitive", api: "TOOL_CLASSIFICATION_SENSITIVE"},
	{name: "dangerous", api: "TOOL_CLASSIFICATION_DANGEROUS"},
}}

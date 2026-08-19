package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var mcpGatewayListToolsCmd = &cobra.Command{
	Use:   "list-tools",
	Short: "List the tools the MCP gateway exposes to you (NDJSON)",
	Long: `Run the MCP handshake against the gateway and list the tools it exposes to
the caller. One NDJSON row per tool (name, description). Use --full to include
each tool's input JSON schema.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		gc, err := newGatewayClient(cmd)
		if err != nil {
			return err
		}
		tools, err := gc.ListTools(cmd.Context())
		if err != nil {
			return fmt.Errorf("tools/list failed: %w", classifyGatewayError(err))
		}

		full, _ := cmd.Flags().GetBool("full")
		enc := newEmitter(cmd.OutOrStdout())
		for _, t := range tools {
			row := map[string]any{"name": t.Name, "description": t.Description}
			if full && len(t.InputSchema) > 0 {
				row["input_schema"] = json.RawMessage(t.InputSchema)
			}
			_ = enc.Encode(row)
		}
		return nil
	},
}

func init() {
	mcpGatewayListToolsCmd.Flags().Bool("full", false, "Include each tool's input JSON schema in the row")
	mcpGatewayCmd.AddCommand(mcpGatewayListToolsCmd)
}

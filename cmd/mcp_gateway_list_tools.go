package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/mcpgateway"
	"github.com/spf13/cobra"
)

var mcpGatewayListToolsCmd = &cobra.Command{
	Use:   "list-tools",
	Short: "List the tools the MCP gateway exposes to you (NDJSON)",
	Long: `Run the MCP handshake against the gateway and list the tools it exposes to
the caller. One NDJSON row per tool (name, description). Use --full to include
each tool's input JSON schema. --limit N prints only the first N tools (0 =
all).`,
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
		return emitGatewayTools(cmd, tools, full, getIntFlag(cmd, "limit"))
	},
}

// emitGatewayTools writes one row per tool, stopping at limit (0 = all).
// tools/list has no page-size parameter, so the cap applies after the
// gateway's pages are fetched.
func emitGatewayTools(cmd *cobra.Command, tools []mcpgateway.Tool, full bool, limit int) error {
	enc := newEmitter(cmd)
	for _, t := range tools {
		if limitReached(enc.Written(), limit) {
			break
		}
		row := map[string]any{"name": t.Name, "description": t.Description}
		if full && len(t.InputSchema) > 0 {
			row["input_schema"] = json.RawMessage(t.InputSchema)
		}
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	mcpGatewayListToolsCmd.Flags().Bool("full", false, "Include each tool's input JSON schema in the row")
	addLimitFlag(mcpGatewayListToolsCmd)
	mcpGatewayCmd.AddCommand(mcpGatewayListToolsCmd)
}

package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var mcpGatewayCallCmd = &cobra.Command{
	Use:   "call <tool-name>",
	Short: "Invoke a tool through the MCP gateway (pretty JSON result)",
	Long: `Invoke a tool exposed by the gateway and print its raw MCP result (the
content array and isError flag). Pass arguments as a JSON object via --args.

Find tool names and their input schemas with "c1i mcp gateway list-tools --full".

  c1i mcp gateway call my_tool --args '{"id":"abc"}'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var raw json.RawMessage
		if argsJSON, _ := cmd.Flags().GetString("args"); strings.TrimSpace(argsJSON) != "" {
			// MCP tool arguments must be a JSON object. json.Valid would also
			// accept an array/string/number/null, which the gateway would then
			// reject with a confusing server-side error — enforce object-ness
			// client-side as a usage error.
			var obj map[string]any
			if err := json.Unmarshal([]byte(argsJSON), &obj); err != nil {
				return &usageError{fmt.Errorf("--args must be a JSON object: %w", err)}
			}
			raw = json.RawMessage(argsJSON)
		}

		gc, err := newGatewayClient(cmd)
		if err != nil {
			return err
		}
		result, err := gc.CallTool(cmd.Context(), args[0], raw)
		if err != nil {
			return fmt.Errorf("tools/call failed: %w", err)
		}
		return writeRawObject(cmd, result)
	},
}

func init() {
	mcpGatewayCallCmd.Flags().String("args", "", "Tool arguments as a JSON object (e.g. '{\"id\":\"abc\"}')")
	mcpGatewayCmd.AddCommand(mcpGatewayCallCmd)
}

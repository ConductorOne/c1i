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
			if obj == nil {
				// json.Unmarshal of `null` into a map succeeds and leaves it nil.
				return &usageError{fmt.Errorf("--args must be a JSON object, not null")}
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
		return renderCallResult(cmd, args[0], result)
	},
}

// renderCallResult prints a successful tools/call result and reports whether
// the tool itself failed. It is split out from RunE so a test can drive it
// directly against a real CallTool result without needing a full CLI
// invocation (which would additionally require authenticating through
// newGatewayClient).
//
// The full result is always printed first, exactly as before this isError
// check existed — an in-band consumer (e.g. an LLM host reading the error
// text out of the content array) must see it unchanged regardless of
// isError. Only after that do we look at isError to decide the process exit
// code, by returning a *toolExecutionError (-> exit 7) rather than nil.
func renderCallResult(cmd *cobra.Command, toolName string, result json.RawMessage) error {
	if err := writeRawObject(cmd, result); err != nil {
		return err
	}
	if toolResultIsError(result) {
		return &toolExecutionError{fmt.Errorf("tool %q reported an error (isError: true); see the result printed above for details", toolName)}
	}
	return nil
}

// toolResultIsError reports whether an MCP tools/call result carries
// isError:true, per the MCP spec's CallToolResult ("isError?: boolean" —
// https://modelcontextprotocol.io/specification/2025-06-18/server/tools):
// absent or false means the tool call succeeded; only an explicit true means
// the tool itself failed (the JSON-RPC call and any transport underneath it
// still succeeded — that's a separate error class, see toolExecutionError). A
// result that isn't a JSON object, or has no isError key, is treated as not
// an error rather than failing closed, matching decodeMessage's tolerance for
// unexpected shapes elsewhere in this package.
func toolResultIsError(result json.RawMessage) bool {
	var probe struct {
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &probe); err != nil {
		return false
	}
	return probe.IsError
}

func init() {
	mcpGatewayCallCmd.Flags().String("args", "", "Tool arguments as a JSON object (e.g. '{\"id\":\"abc\"}')")
	mcpGatewayCmd.AddCommand(mcpGatewayCallCmd)
}

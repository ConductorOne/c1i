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
			return fmt.Errorf("tools/call failed: %w", classifyGatewayError(err))
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
	switch classifyIsError(result) {
	case isErrorTrue:
		return &toolExecutionError{fmt.Errorf("tool %q reported an error (isError: true); see the result printed above for details", toolName)}
	case isErrorMalformed:
		return &toolExecutionError{fmt.Errorf("tool %q returned a non-boolean isError value; MCP defines isError as a boolean, so this is a non-conformant server response — treating it as an error; see the result printed above for details", toolName)}
	}
	return nil
}

// isErrorVerdict is the result of inspecting an MCP tools/call result's
// isError field.
type isErrorVerdict int

const (
	isErrorOK        isErrorVerdict = iota // absent, null, or false: the tool call succeeded
	isErrorTrue                            // isError: true: the tool itself reported a failure
	isErrorMalformed                       // isError present but not a JSON boolean literal (non-conformant server)
)

// classifyIsError inspects an MCP tools/call result's isError field, per the
// MCP spec's CallToolResult ("isError?: boolean" —
// https://modelcontextprotocol.io/specification/2025-06-18/server/tools) —
// and reports which of three outcomes applies:
//
//   - absent, null, or the literal false -> isErrorOK (success)
//   - the literal true -> isErrorTrue (the tool itself failed; the JSON-RPC
//     call and any transport underneath it still succeeded — that's a
//     separate error class, see toolExecutionError)
//   - present but not a JSON boolean literal at all (a string, number,
//     object, or array) -> isErrorMalformed
//
// The isErrorMalformed case is a deliberate tradeoff, not an oversight: MCP
// defines isError as a boolean, so any server sending a non-boolean value is
// already non-conformant, and json.Unmarshal into a bool field fails on that
// type mismatch. Rather than let that decode failure fail open (as it used
// to — silently returning false, which reintroduced the exact "tool failure
// read as success" bug exit code 7 exists to catch), any non-boolean value is
// now treated as an error. This means a spec-violating falsy value like
// isError: 0 is reported as an error too; that tradeoff was accepted
// deliberately in favor of never failing open on a malformed server.
//
// A result that isn't a JSON object at all (or has no isError key at all —
// which json.Unmarshal treats as a top-level decode success that simply
// leaves probe.IsError untouched) is treated as isErrorOK rather than
// failing closed, matching decodeMessage's tolerance for unexpected shapes
// elsewhere in this package.
func classifyIsError(result json.RawMessage) isErrorVerdict {
	var probe struct {
		IsError json.RawMessage `json:"isError"`
	}
	if err := json.Unmarshal(result, &probe); err != nil {
		// Not a JSON object at all (array, string, number, null, or invalid/
		// empty JSON): treated as success, not failing closed.
		return isErrorOK
	}
	switch strings.TrimSpace(string(probe.IsError)) {
	case "", "null", "false":
		return isErrorOK
	case "true":
		return isErrorTrue
	default:
		return isErrorMalformed
	}
}

func init() {
	mcpGatewayCallCmd.Flags().String("args", "", "Tool arguments as a JSON object (e.g. '{\"id\":\"abc\"}')")
	mcpGatewayCmd.AddCommand(mcpGatewayCallCmd)
}

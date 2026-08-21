package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Process exit codes. Agents branch on these instead of parsing stderr: e.g. 5
// means "rate limited, back off and retry", 3 means "re-authenticate". Keep
// these stable.
const (
	exitOK          = 0
	exitError       = 1 // generic / unclassified failure
	exitUsage       = 2 // bad flags or arguments
	exitAuth        = 3 // not authenticated, or API returned 401/403
	exitNotFound    = 4 // API returned 404
	exitRateLimited = 5 // API returned 429
	exitServer      = 6 // C1 failed: 5xx, a redirect loop, or a 200 with a non-JSON body
	exitToolError   = 7 // an MCP tool call completed (transport/protocol succeeded) but the tool itself reported isError
	exitUpstream    = 8 // an upstream system, or the protocol layer itself, failed -- distinct from the C1 API failing (e.g. an unreachable MCP connector, or a JSON-RPC protocol-level error)
)

// usageError marks an error as a CLI-usage problem (bad flag/args) so it maps to
// exitUsage. It is installed via cobra's FlagErrorFunc.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// toolExecutionError marks an MCP tool call (`mcp gateway call`) whose result
// carried isError:true — the JSON-RPC request succeeded (and, if it went over
// HTTP, so did that), but the tool itself reported a failure. This is a
// different class of failure from the transport/protocol codes (3/4/5/6): the
// call completed; the *tool* failed. It maps to its own exitToolError so an
// agent branching on exit code can tell the two apart. Not exported: nothing
// outside cmd constructs one today, and keeping it here means exitCode's
// switch is the single place that has to agree with cmd/mcp_gateway_call.go
// on what it means.
type toolExecutionError struct{ err error }

func (e *toolExecutionError) Error() string { return e.err.Error() }
func (e *toolExecutionError) Unwrap() error { return e.err }

// nonJSONResponseError marks an HTTP-success response whose body isn't valid
// JSON (e.g. a path that escapes the API prefix and lands on a server that
// answers 200 with an HTML document). Maps to exitServer: the request itself
// was well-formed, so this isn't a usage error; it's the remote side failing
// to honor the JSON contract this CLI depends on, the same "remote responded
// but not usefully" bucket exitServer already covers for 5xx.
type nonJSONResponseError struct{ err error }

func (e *nonJSONResponseError) Error() string { return e.err.Error() }
func (e *nonJSONResponseError) Unwrap() error { return e.err }

// upstreamError marks a failure beyond C1, or in the protocol layer, that did
// not arrive as a classifiable HTTP status — an upstream connector failing, or
// a JSON-RPC protocol error. It maps to exitUpstream (8), distinct from C1
// itself failing (exitServer, 6). These arrive on an HTTP 200, so wrapping in
// *client.APIError would mean inventing a status that then renders as a false
// "status" field in --error-format json.
type upstreamError struct{ err error }

func (e *upstreamError) Error() string { return e.err.Error() }
func (e *upstreamError) Unwrap() error { return e.err }

// Run executes the root command, renders any error per --error-format, and
// returns the process exit code. main() is just os.Exit(cmd.Run()).
//
// cmd.Context() is wired to cancel on SIGINT/SIGTERM (first Ctrl-C cancels
// gracefully; a second reverts to the OS default hard-kill), so long-running
// commands — e.g. "apps set-owners --wait" polling for async provisioning —
// can honor cancellation instead of only a --*-timeout flag.
func Run() int {
	// Cobra creates "completion" lazily inside ExecuteC, i.e. after the guards
	// below would have run -- so it never got the synthetic RunE that turns an
	// unknown subcommand into a usage error, and "completion bogus-shell" printed
	// help and exited 0. Creating it here puts it under the same guard as every
	// other group. Deliberately not InitDefaultHelpCmd: cobra's help is runnable
	// with a nil Args, so the NoArgs stamp below would break "c1i help <command>".
	rootCmd.InitDefaultCompletionCmd()
	attachSubcommandGuards(rootCmd)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop() // after the first signal, let a second Ctrl-C hit the default handler (hard-kill)
	}()

	err := rootCmd.ExecuteContext(ctx)
	if err == nil {
		return exitOK
	}
	writeError(os.Stderr, err, viper.GetString("error_format"))
	return exitCode(err)
}

// attachSubcommandGuards walks the whole command tree once (at Run()) and
// closes two gaps that otherwise read as silent success:
//
//   - A command group (has subcommands, no Run/RunE of its own) fails on an
//     unknown subcommand instead of printing help and exiting 0. Without
//     this, `c1i mcp bogus` reads as success. Running a group with no args
//     still prints help (exit 0).
//   - A runnable command that leaves Args unset falls back to cobra's
//     ArbitraryArgs, so `c1i mcp servers list somejunk` silently ignores the
//     stray positional and exits 0. Any runnable leaf whose Args is nil gets
//     cobra.NoArgs here so a stray positional becomes a usage error (exit 2)
//     instead. This only ever sets Args when it is nil — a command that
//     already declares its own Args (e.g. a migrated `get <id>` command using
//     cobra.ExactArgs(1)) is never touched.
func attachSubcommandGuards(c *cobra.Command) {
	for _, sub := range c.Commands() {
		attachSubcommandGuards(sub)
	}
	// Capture runnability before the group guard below potentially installs a
	// synthetic RunE — otherwise a pure group command would look "runnable"
	// by the time we get to the NoArgs check, and cobra.NoArgs would replace
	// the synthetic RunE's own usageError-wrapped "unknown subcommand"
	// message with cobra's plain "unknown command" one before RunE ever runs
	// (ValidateArgs happens first). Groups keep their existing behavior.
	wasRunnable := c.Runnable()
	if c.HasSubCommands() && c.Run == nil && c.RunE == nil {
		c.RunE = func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return &usageError{fmt.Errorf("unknown subcommand %q for %q", args[0], cmd.CommandPath())}
		}
	}
	// Defaulting nil Args to NoArgs is only safe because a command that
	// legitimately takes a positional is required to set Args itself —
	// TestArgsUseConsistencyAcrossTree (cmd/args_positional_test.go) fails CI
	// if a Use string documenting "<id>" isn't backed by a matching Args. Do
	// not remove this stamp; it's what makes leaving Args unset elsewhere safe.
	if wasRunnable && c.Args == nil {
		c.Args = cobra.NoArgs
	}
}

// exitCode classifies err into a stable process exit code.
func exitCode(err error) int {
	if err == nil {
		return exitOK
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == 401 || apiErr.StatusCode == 403:
			return exitAuth
		case apiErr.StatusCode == 404:
			return exitNotFound
		case apiErr.StatusCode == 429:
			return exitRateLimited
		// 408 and 499 both mean the request ended without the caller having
		// done anything wrong, so neither is a usage error; and neither is
		// evidence that C1 itself failed, so neither is exitServer. Both
		// fall to the generic exitError. (This API has been observed to
		// produce both, from different error paths.)
		case apiErr.StatusCode == 408 || apiErr.StatusCode == 499:
			return exitError
		// Every other 4xx (400, 409, 413, 414, 422, ...) means the caller sent
		// something the server rejected -- a usage error, not exitError. A
		// per-status list would drift; this rule covers whatever the API adds
		// next too.
		case apiErr.StatusCode >= 400 && apiErr.StatusCode < 500:
			return exitUsage
		case apiErr.StatusCode >= 500:
			return exitServer
		default:
			return exitError
		}
	}
	var authErr *client.AuthError
	if errors.As(err, &authErr) {
		return exitAuth
	}
	var pathErr *client.PathError
	if errors.As(err, &pathErr) {
		return exitUsage
	}
	var redirErr *client.RedirectError
	if errors.As(err, &redirErr) {
		return exitUsage
	}
	var redirLoopErr *client.RedirectLoopError
	if errors.As(err, &redirLoopErr) {
		return exitServer
	}
	var toolErr *toolExecutionError
	if errors.As(err, &toolErr) {
		return exitToolError
	}
	var upstreamErr *upstreamError
	if errors.As(err, &upstreamErr) {
		return exitUpstream
	}
	var nonJSONErr *nonJSONResponseError
	if errors.As(err, &nonJSONErr) {
		return exitServer
	}
	var usageErr *usageError
	if errors.As(err, &usageErr) {
		return exitUsage
	}
	if isCobraUsageError(err) {
		return exitUsage
	}
	return exitError
}

// isCobraUsageError matches the usage failures cobra returns as plain errors
// (not through FlagErrorFunc): unknown command, unknown/malformed flag, missing
// required flag, and wrong argument count. These are only checked after the
// typed API/auth/usage errors above, so an API error body containing one of
// these substrings can't be misclassified.
func isCobraUsageError(err error) bool {
	msg := err.Error()
	for _, p := range []string{
		"unknown command",
		"unknown flag",
		"unknown shorthand flag",
		"required flag(s)",
		"flag needs an argument",
		"invalid argument",
		"arg(s)", // cobra's ExactArgs/MinimumNArgs/etc. messages
	} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// writeError renders err to w. With format=="json" it emits a single JSON object
// ({error, and for API errors status/method/path/body}); otherwise the familiar
// "Error: <msg>" text line.
func writeError(w io.Writer, err error, format string) {
	if strings.EqualFold(format, "json") {
		obj := map[string]any{"error": displayError(err).Error()}
		var apiErr *client.APIError
		if errors.As(err, &apiErr) {
			obj["status"] = apiErr.StatusCode
			obj["method"] = apiErr.Method
			obj["path"] = apiErr.Path
			if apiErr.Body != "" {
				obj["body"] = rawJSONOrString(apiErr.Body)
			}
		}
		if b, mErr := json.Marshal(obj); mErr == nil {
			_, _ = fmt.Fprintln(w, string(b))
			return
		}
	}
	_, _ = fmt.Fprintf(w, "Error: %v\n", displayError(err))
}

// displayError returns the error to print. A *client.PathError never reaches
// the wire — do() refuses the request before sending — so any "API error:"
// (or similar) prefix a call site wrapped it in is a false claim; print the
// PathError's own message instead of the full wrapped chain. A
// *client.RedirectError and *client.RedirectLoopError get the same
// treatment: both are the client's own refusal to follow a 3xx (outright, or
// after chasing a canonicalization chain too far), not a response the
// wrapping "API error:" prefix accurately describes either.
func displayError(err error) error {
	var pathErr *client.PathError
	if errors.As(err, &pathErr) {
		return pathErr
	}
	var redirErr *client.RedirectError
	if errors.As(err, &redirErr) {
		return redirErr
	}
	var redirLoopErr *client.RedirectLoopError
	if errors.As(err, &redirLoopErr) {
		return redirLoopErr
	}
	return err
}

// rawJSONOrString embeds s as structured JSON when it is a JSON object/array, so
// an API error body isn't double-escaped into a string; otherwise returns the
// plain string.
func rawJSONOrString(s string) any {
	t := strings.TrimSpace(s)
	if (strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")) && json.Valid([]byte(t)) {
		return json.RawMessage(t)
	}
	return s
}

package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/mcpgateway"
	"github.com/spf13/cobra"
)

var mcpGatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Call the C1 MCP gateway (list and invoke tools end to end)",
	Long: `Drive the C1 MCP gateway over its streamable-HTTP MCP transport — the same
handshake an MCP host performs (initialize -> notifications/initialized ->
tools/list / tools/call) — to verify what a registered server actually exposes.
This closes the configure-then-verify loop: register a server, approve its
tools, then list/call them here.

The gateway URL is derived from --url / C1I_URL by inserting "-mcp" into the
host (https://acme.conductor.one -> https://acme-mcp.conductor.one/v1);
override it with --gateway-url. Auth uses your stored C1 credentials — the
standard API token is accepted by the gateway, so no extra setup is needed.

Subcommands:
  list-tools  - List the tools the gateway exposes to you (NDJSON)
  call        - Invoke a tool and print its result`,
}

// deriveGatewayURL turns an API base URL into the MCP gateway endpoint by
// inserting "-mcp" before the first dot of the host and appending /v1
// (https://acme.conductor.one -> https://acme-mcp.conductor.one/v1).
func deriveGatewayURL(baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("cannot derive gateway URL from %q", baseURL)
	}
	// Insert -mcp into the hostname (not the port): acme.conductor.one ->
	// acme-mcp.conductor.one.
	hostname := u.Hostname()
	if i := strings.Index(hostname, "."); i > 0 {
		hostname = hostname[:i] + "-mcp" + hostname[i:]
	} else {
		hostname += "-mcp"
	}
	host := hostname
	if p := u.Port(); p != "" {
		host = hostname + ":" + p
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/v1", scheme, host), nil
}

// newGatewayClient resolves the gateway endpoint (--gateway-url or derived from
// --url), mints a bearer for the API host (which the gateway accepts), and
// returns an MCP client that has completed the initialize handshake.
func newGatewayClient(cmd *cobra.Command) (*mcpgateway.Client, error) {
	baseURL, err := GetBaseURL()
	if err != nil {
		return nil, err
	}
	endpoint, _ := cmd.Flags().GetString("gateway-url")
	if endpoint == "" {
		endpoint, err = deriveGatewayURL(baseURL)
		if err != nil {
			return nil, err
		}
	}
	tok, err := client.Token(cmd.Context(), baseURL)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}
	gc := mcpgateway.New(endpoint, tok.AccessToken, nil)
	// *mcpgateway.HTTPError unwraps to a *client.APIError, so wrapping with %w
	// here is enough for cmd/errors.go's exitCode to classify a gateway 401/403
	// as auth, 404 as not-found, 429 as rate-limited, and 5xx as server — the
	// same taxonomy every other API failure gets. No separate classification
	// helper is needed, and none is needed at the list-tools/call call sites
	// either, since they wrap gateway errors with %w too.
	if err := gc.Initialize(cmd.Context()); err != nil {
		return nil, fmt.Errorf("gateway handshake failed: %w", classifyGatewayError(err))
	}
	return gc, nil
}

// classifyGatewayError reclassifies a gateway-layer error so it reaches the
// right process exit code through cmd/errors.go's exitCode:
//
//   - *mcpgateway.TransportError (a dial failure, connection refused, TLS
//     failure, or timeout — the request never got an HTTP response at all,
//     let alone a JSON-RPC one): wrapped in *upstreamError -> exit 8. This is
//     a system beyond C1 (or the network path to it) failing, the same class
//     exit 8 already covers for a JSON-RPC-level protocol failure; it is
//     checked first; and deliberately does not touch *client.AuthError (still
//     exit 3, e.g. a rejected client_credentials grant during token minting,
//     which happens before this function ever runs) or *mcpgateway.HTTPError
//     (still classified via its own Unwrap to *client.APIError — a real
//     response with a real status arrived, which is a different failure
//     class than a transport failure).
//
// The remaining cases classify a JSON-RPC-level error from the gateway (an
// *mcpgateway.rpcError, unexported — accessed via RPCErrorCode):
//
//   - -32602 (invalid params): the caller named a tool with bad arguments —
//     that IS caller-caused. Wrapped in *usageError -> exit 2.
//   - -32601 (method not found): this client only ever sends four fixed,
//     spec-required methods (initialize, notifications/initialized,
//     tools/list, tools/call — see mcpgateway's outboundRPCMethods), so a
//     caller cannot trigger this themselves — it firing means a
//     protocol-version mismatch or a bug in this CLI, not a bad invocation.
//     Wrapped in *upstreamError -> exit 8 (moved off exitUsage, which would
//     otherwise send the user hunting their own command line for a problem
//     that isn't there). TestOutboundRPCMethods in
//     internal/mcpgateway/gateway_test.go pins that method set: adding a new
//     outbound method invalidates this premise and means this case needs
//     revisiting.
//   - -32700 (parse error) / -32600 (invalid request): protocol-level
//     failures. Wrapped in *upstreamError -> exit 8.
//   - a JSON-RPC code that is present and 0 (an upstream connector failure —
//     an unreachable external MCP server, a vendor API error surfaced
//     through the connector, ...). Wrapped in *upstreamError -> exit 8. This
//     arrives on an HTTP 200 response (the gateway itself didn't fail), so
//     it is NOT wrapped in a *client.APIError with an invented status — that
//     was tried and reverted because it rendered as a false "status" field
//     in --error-format json.
//   - any other code, a JSON-RPC error object with no `code` field at all
//     (RPCErrorCode returns ok=true, code=nil — distinct from a present 0),
//     or no JSON-RPC code whatsoever (e.g. a transport-level
//     *mcpgateway.HTTPError, which already classifies via its own Unwrap to
//     *client.APIError with a real status): left unchanged, exits 1
//     (generic) as before.
//
// *usageError and *upstreamError both live in package cmd (internal/mcpgateway
// must not import cmd), so this reclassification has to happen here rather
// than in the gateway client — this function is the seam. Call it on the
// error CallTool/ListTools return, before wrapping with the "%s failed: %w"
// context each call site already adds; both wrapper types' Error() delegates
// verbatim to the wrapped error, so the rendered message is unchanged.
func classifyGatewayError(err error) error {
	if err == nil {
		return nil
	}
	var transportErr *mcpgateway.TransportError
	if errors.As(err, &transportErr) {
		return &upstreamError{err}
	}
	code, ok := mcpgateway.RPCErrorCode(err)
	if !ok || code == nil {
		return err
	}
	switch *code {
	case -32602:
		return &usageError{err}
	case -32601, -32700, -32600, 0:
		return &upstreamError{err}
	}
	return err
}

func init() {
	mcpGatewayCmd.PersistentFlags().String("gateway-url", "", "MCP gateway endpoint (default: derived from --url by inserting -mcp into the host)")
	mcpCmd.AddCommand(mcpGatewayCmd)
}

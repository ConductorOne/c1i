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
	// Insert -mcp into the hostname (not the port): leet.conductor.one ->
	// leet-mcp.conductor.one.
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
	if err := gc.Initialize(cmd.Context()); err != nil {
		return nil, fmt.Errorf("gateway handshake failed: %w", classifyGatewayErr(err))
	}
	return gc, nil
}

// classifyGatewayErr maps a gateway 401/403 to a client.AuthError so it reaches
// the exit-3 (auth) classification in cmd/errors.go — the gateway uses the same
// bearer as the API, so an auth rejection there is an auth failure. Other errors
// pass through unchanged.
func classifyGatewayErr(err error) error {
	var he *mcpgateway.HTTPError
	if errors.As(err, &he) && (he.StatusCode == 401 || he.StatusCode == 403) {
		return &client.AuthError{Err: err}
	}
	return err
}

func init() {
	mcpGatewayCmd.PersistentFlags().String("gateway-url", "", "MCP gateway endpoint (default: derived from --url by inserting -mcp into the host)")
	mcpCmd.AddCommand(mcpGatewayCmd)
}

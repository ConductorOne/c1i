package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpServersRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a new MCP server under an app (pretty JSON)",
	Long: `Register a new HOSTED or EXTERNAL MCP server under an existing app.

HOSTED servers run inside C1 from a catalog impl — browse
"c1i mcp servers catalog list" and pass the entry's id via --catalog-id.
EXTERNAL servers point at a third-party MCP URL via --server-url.

Auth (convenience flags cover the simple methods):
  --auth none
  --auth bearer-token  --bearer-token TOKEN
  --auth custom-header --header-name NAME --header-value VALUE
  --auth basic-auth    --basic-auth-username USER --basic-auth-password PASS

Each companion value is required (a blank one exits 2 before sending) unless
--token-sharing per-user, where each user supplies the secret later via
SubmitUserCredential. Register takes no --update-mask, so it always needs the
full arm (a custom header's name even under per-user).

For OAuth2 / AWS SigV4 / Google service-account auth, pass the full config
object via --hosted-config-file / --external-config-file (JSON, or "-" for
stdin). Don't hand-write it — generate a ready-to-edit skeleton:

  c1i mcp servers register --print-config-template --auth oauth2 [--type hosted]

The hostedConfig/externalConfig object nests auth under a single arm keyed by
method (exactly one of the following; field names shown for reference):
  oauth2:               {mode, clientId, clientSecret, authorizeUrl, tokenUrl,
                         issuerUrl, scopes[], pkce}
  awsSigv4:             {accessKeyId, secretAccessKey, sessionToken}
  googleServiceAccount: {credentialsJson, scopes[]}
  bearerToken:          {token}
  customHeader:         {headerName, headerValue}
  basicAuth:            {username, password}
  none:                 {}
oauth2.mode is one of MCP_SERVER_AUTH_OAUTH2_MODE_{SERVICE,PASSTHROUGH,
CLIENT_CREDENTIALS,JWT_BEARER,GOOGLE_SERVICE_ACCOUNT,AUTHORIZATION_CODE}.

tokenSharing x auth-method compatibility: PER_USER (config value
MCP_SERVER_TOKEN_SHARING_PER_USER) is only valid with oauth2 (authorization-
code/passthrough), bearerToken, customHeader, or basicAuth. Auth secrets are
sealed server-side; reads only return *_configured.

See full schema: c1i docs page api-reference/mcp-servers/register

Prints the created server as pretty JSON, unwrapped like "mcp servers get": the
id is .connectorId (there is no .id). --fields is a usage error on a mutation;
extract with jq -er, which fails on a --dry-run preview where jq -r exits 0
with null.

--wait polls "mcp servers list --app-id" until the new server appears in it
(--wait-timeout bounds the wait). A list read right after register can omit the
server for up to about a second while "mcp servers get" already returns it.

NOTE: registering under app_id="" (new managed app) or via an
app_managed_state_binding_ref is not reachable over REST — this command
requires --app-id. Approve discovered tools afterward with
"c1i mcp tools approve".`,
	RunE: func(cmd *cobra.Command, args []string) error {
		wait, waitTimeout, err := waitFlagValues(cmd)
		if err != nil {
			return err
		}
		if printTemplate, _ := cmd.Flags().GetBool("print-config-template"); printTemplate {
			return printConfigTemplate(cmd)
		}

		if err := requireNonEmpty(cmd, "app-id", "type", "display-name"); err != nil {
			// Wrap as a usage error so a missing required flag still maps to
			// exit code 2 (these flags are annotate-only, not cobra-required,
			// so --print-config-template can short-circuit above them).
			return &usageError{err}
		}

		appID, _ := cmd.Flags().GetString("app-id")
		body, err := buildRegisterBody(cmd)
		if err != nil {
			// buildRegisterBody only fails on bad input (invalid --type,
			// missing --catalog-id/--server-url, unreadable/invalid config file), so
			// map these to the usage exit code (2) like the flag checks above.
			return &usageError{err}
		}

		path := client.Path("/api/v1/apps/%s/mcp_servers", appID)
		// buildRegisterBody already validated the flag, so the error is nil.
		var opts []mutationOpt
		if owners, _ := repeatableStringFlag(cmd, "user-id"); len(owners) > 0 {
			opts = append(opts, withPreRead(existingUsersCheck("user-id", owners)))
		}
		opts = append(opts, withWaitOnResponse(func(c *client.Client, resp []byte) waitOp[[]string] {
			return serverListedWaitOp(c, appID, unwrapServerID(resp), waitTimeout)
		}, wait))
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body, Unwrap: byID("connectorId")}, opts...)
	},
}

// mcpServerWaitInterval is how often register --wait re-lists. A var so a test
// can poll fast.
var mcpServerWaitInterval = time.Second

// unwrapServerID reads .connectorId from a register response, which wraps the
// server ("" if absent).
func unwrapServerID(resp []byte) string {
	var r struct {
		ConnectorID string `json:"connectorId"`
	}
	_ = json.Unmarshal(unwrapEnvelope(resp, "connectorId"), &r)
	return r.ConnectorID
}

// listServerIDs pages GET .../mcp_servers to completion: the new server can
// sit past page one.
func listServerIDs(ctx context.Context, c *client.Client, appID string) ([]string, error) {
	route := "GET " + client.Path("/api/v1/apps/%s/mcp_servers", appID)
	var all []string
	pageToken := ""
	guard := newPaginationCursorGuard()
	for {
		if guard.tokenAlreadyUsed(pageToken) {
			return nil, guard.stuckError(route)
		}
		params := map[string]string{"page_size": strconv.Itoa(100)}
		if pageToken != "" {
			params["page_token"] = pageToken
		}
		data, err := c.Get(ctx, client.Path("/api/v1/apps/%s/mcp_servers", appID), params)
		if err != nil {
			return nil, fmt.Errorf("API error: %w", err)
		}
		var resp struct {
			List          []serverView `json:"list"`
			NextPageToken string       `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}
		ids := make([]string, len(resp.List))
		for i, it := range resp.List {
			ids[i] = it.ConnectorID
		}
		keep, err := guard.checkPage(nil, route, ids)
		if err != nil {
			return nil, err
		}
		all = append(all, keepRows(ids, keep)...)
		if resp.NextPageToken == "" {
			return all, nil
		}
		guard.markTokenUsed(pageToken)
		pageToken = resp.NextPageToken
	}
}

func serverListedWaitOp(c *client.Client, appID, connectorID string, timeout time.Duration) waitOp[[]string] {
	return waitOp[[]string]{
		Poll:     func(ctx context.Context) ([]string, error) { return listServerIDs(ctx, c, appID) },
		Done:     untilPresent([]string{connectorID}),
		Interval: mcpServerWaitInterval,
		Timeout:  timeout,
		Subject:  "MCP server " + connectorID + " to appear in the app's server list",
		Success:  "MCP server " + connectorID + " is listed",
		Slow:     "the list can trail a register by a few seconds",
		Recheck:  "c1i mcp servers list --app-id " + appID,
	}
}

// buildRegisterBody assembles the RegisterRequest body from flags. Pure (no
// network / auth), so the dry-run preview and unit tests exercise the same
// path the live request takes.
func buildRegisterBody(cmd *cobra.Command) (map[string]any, error) {
	appID, _ := cmd.Flags().GetString("app-id")
	typ, err := serverTypeFlag.parse(mustGetString(cmd, "type"))
	if err != nil {
		return nil, err
	}
	displayName, _ := cmd.Flags().GetString("display-name")

	body := map[string]any{
		"appId":       appID,
		"serverType":  typ,
		"displayName": displayName,
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if v, err := dataSensitivityFlag.parse(mustGetString(cmd, "data-sensitivity")); err != nil {
		return nil, err
	} else if v != "" {
		body["dataSensitivity"] = v
	}
	if v, _ := cmd.Flags().GetString("tool-prefix"); v != "" {
		body["toolPrefix"] = v
	}
	ids, err := repeatableStringFlag(cmd, "user-id")
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		body["userIds"] = ids
	}

	// serverTypeFlag rejects unknowns, so typ is HOSTED or EXTERNAL.
	switch typ {
	case "MCP_SERVER_TYPE_HOSTED":
		cfg, err := buildHostedConfig(cmd)
		if err != nil {
			return nil, err
		}
		if !flagGiven(cmd, "hosted-config-file") {
			if _, ok := cfg["mcpServerCatalogId"]; !ok {
				return nil, fmt.Errorf("--catalog-id is required for --type hosted (or pass --hosted-config-file)")
			}
		}
		body["hostedConfig"] = cfg
	case "MCP_SERVER_TYPE_EXTERNAL":
		cfg, err := buildExternalConfig(cmd)
		if err != nil {
			return nil, err
		}
		if !flagGiven(cmd, "external-config-file") {
			if _, ok := cfg["url"]; !ok {
				return nil, fmt.Errorf("--server-url is required for --type external (or pass --external-config-file)")
			}
		}
		body["externalConfig"] = cfg
	}

	return body, nil
}

func init() {
	mcpServersRegisterCmd.Flags().String("app-id", "", "Application ID to register under")
	mcpServersRegisterCmd.Flags().String("type", "", "Server type: "+serverTypeFlag.usage())
	mcpServersRegisterCmd.Flags().String("display-name", "", "Display name ("+displayNameRule+")")
	mcpServersRegisterCmd.Flags().String("description", "", "Description")
	mcpServersRegisterCmd.Flags().String("data-sensitivity", "", "Data sensitivity: "+dataSensitivityFlag.usage())
	mcpServersRegisterCmd.Flags().String("tool-prefix", "", "Prefix for exposed tool names")
	addRepeatableStringFlag(mcpServersRegisterCmd, "user-id", "Integration owner user ID (repeatable)")
	// HOSTED config
	mcpServersRegisterCmd.Flags().String("catalog-id", "", "Catalog entry ID (HOSTED)")
	mcpServersRegisterCmd.Flags().String("source-app-id", "", "Source app ID for connector-backed HOSTED servers")
	addRepeatableStringFlag(mcpServersRegisterCmd, "config-field", "Extra config field key=value (HOSTED, repeatable)")
	mcpServersRegisterCmd.Flags().String("hosted-config-file", "", "Full hostedConfig JSON (file or \"-\" for stdin)")
	// EXTERNAL config
	mcpServersRegisterCmd.Flags().String("server-url", "", "External MCP server URL (EXTERNAL)")
	mcpServersRegisterCmd.Flags().String("transport", "", "Transport: "+transportTypeFlag.usage()+" (EXTERNAL)")
	mcpServersRegisterCmd.Flags().String("external-config-file", "", "Full externalConfig JSON (file or \"-\" for stdin)")
	allowEmpty(mcpServersRegisterCmd, emptyIsOmitted, "description", "data-sensitivity", "tool-prefix", "server-url", "transport")
	// Config-template generator (no auth / no network)
	mcpServersRegisterCmd.Flags().Bool("print-config-template", false, "Print a ready-to-edit config skeleton for the --auth method (use with --auth and --type) instead of registering")
	// Shared auth
	addAuthFlags(mcpServersRegisterCmd)
	addWaitFlags(mcpServersRegisterCmd, "GET .../mcp_servers until the new server is listed", time.Minute)
	// annotate-only (not cobra-required) so --print-config-template works
	// without --app-id/--type/--display-name; RunE enforces them otherwise.
	annotateRequired(mcpServersRegisterCmd, "app-id", "type", "display-name")
	markMutating(mcpServersRegisterCmd)
	markUserRefChecked(mcpServersRegisterCmd, "user-id")
	mcpServersCmd.AddCommand(mcpServersRegisterCmd)
}

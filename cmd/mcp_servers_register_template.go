package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// authArmTemplate returns the oneof auth arm (keyed by its proto-JSON case name)
// for the given --auth mode, pre-filled with placeholders. The field names and
// enum values mirror what `register` sends on the wire and what `catalog get`
// advertises, so a filled-in template round-trips through --hosted-config-file /
// --external-config-file unchanged. Returns an error for an unknown mode so the
// template never emits a silently wrong shape.
func authArmTemplate(mode string) (map[string]any, error) {
	switch strings.ToLower(mode) {
	case "", "none":
		return map[string]any{"none": map[string]any{}}, nil
	case "bearer-token", "bearer":
		return map[string]any{"bearerToken": map[string]any{
			"token": "<bearer-token>",
		}}, nil
	case "custom-header":
		return map[string]any{"customHeader": map[string]any{
			"headerName":  "<header-name>",
			"headerValue": "<header-value>",
		}}, nil
	case "basic-auth", "basic":
		return map[string]any{"basicAuth": map[string]any{
			"username": "<username>",
			"password": "<password>",
		}}, nil
	case "oauth2":
		return map[string]any{"oauth2": map[string]any{
			// SERVICE = one shared credential; switch to
			// MCP_SERVER_AUTH_OAUTH2_MODE_PASSTHROUGH (+ tokenSharing PER_USER)
			// for per-user authorization-code flows.
			"mode":         "MCP_SERVER_AUTH_OAUTH2_MODE_SERVICE",
			"clientId":     "<oauth-client-id>",
			"clientSecret": "<oauth-client-secret>",
			"authorizeUrl": "https://provider.example/oauth/authorize",
			"tokenUrl":     "https://provider.example/oauth/token",
			"scopes":       []string{"<scope>"},
		}}, nil
	case "aws-sigv4", "aws":
		return map[string]any{"awsSigv4": map[string]any{
			"accessKeyId":     "<aws-access-key-id>",
			"secretAccessKey": "<aws-secret-access-key>",
			"sessionToken":    "",
		}}, nil
	case "google-service-account", "google":
		return map[string]any{"googleServiceAccount": map[string]any{
			"credentialsJson": "<service-account-credentials-json-string>",
			"scopes":          []string{"<scope>"},
		}}, nil
	default:
		return nil, fmt.Errorf("unknown --auth %q for --print-config-template: use none, bearer-token, custom-header, basic-auth, oauth2, aws-sigv4, or google-service-account", mode)
	}
}

// printConfigTemplate emits a ready-to-edit config skeleton for
// --hosted-config-file / --external-config-file to stdout (valid JSON, so it
// pipes to a file), with human guidance on stderr. It performs no auth and no
// network I/O.
func printConfigTemplate(cmd *cobra.Command) error {
	serverType, _ := cmd.Flags().GetString("type")
	if serverType == "" {
		serverType = "hosted"
	}
	auth, _ := cmd.Flags().GetString("auth")

	arm, err := authArmTemplate(auth)
	if err != nil {
		return err
	}

	cfg := map[string]any{}
	switch strings.ToLower(serverType) {
	case "hosted":
		cfg["mcpServerCatalogId"] = "<catalog entry id — see: c1i mcp servers catalog list>"
		cfg["tokenSharing"] = "MCP_SERVER_TOKEN_SHARING_SHARED"
	case "external":
		cfg["url"] = "https://your-mcp-server.example/mcp"
		cfg["transportType"] = "MCP_SERVER_TRANSPORT_TYPE_STREAMABLE_HTTP"
	default:
		return fmt.Errorf("invalid --type %q: use hosted or external", serverType)
	}
	for k, v := range arm {
		cfg[k] = v
	}

	// Encode with HTML escaping off so <placeholders>, > and — survive as
	// literal characters an agent (or human) can read and replace.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return err
	}

	flagName := "hosted-config-file"
	if strings.EqualFold(serverType, "external") {
		flagName = "external-config-file"
	}
	authLabel := auth
	if authLabel == "" {
		authLabel = "none"
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"# %s %s config template. Replace <placeholders>, then:\n#   c1i mcp servers register --app-id APP --type %s --display-name NAME --%s config.json\n# Fields with <angle-brackets> are placeholders; enum/URL values are examples. Secrets are sealed server-side.\n",
		serverType, authLabel, strings.ToLower(serverType), flagName)
	_, err = fmt.Fprint(cmd.OutOrStdout(), buf.String())
	return err
}

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
	arm := "none"
	if mode != "" {
		var err error
		if arm, err = authTemplateFlag.parse(mode); err != nil {
			return nil, fmt.Errorf("unknown --auth %q for --print-config-template: use %s", mode, authTemplateFlag.usage())
		}
	}
	switch arm {
	case "none":
		return map[string]any{"none": map[string]any{}}, nil
	case "bearerToken":
		return map[string]any{"bearerToken": map[string]any{
			"token": "<bearer-token>",
		}}, nil
	case "customHeader":
		return map[string]any{"customHeader": map[string]any{
			"headerName":  "<header-name>",
			"headerValue": "<header-value>",
		}}, nil
	case "basicAuth":
		return map[string]any{"basicAuth": map[string]any{
			"username": "<username>",
			"password": "<password>",
		}}, nil
	case "oauth2":
		return map[string]any{"oauth2": map[string]any{
			// SERVICE = one shared service credential. For per-user auth (each
			// user authorizes individually), use ..._MODE_PASSTHROUGH with
			// tokenSharing PER_USER. SERVICE/PASSTHROUGH are the input modes a
			// catalog entry advertises (supportedOauth2Modes); the backend may
			// store a *resolved* grant such as ..._MODE_AUTHORIZATION_CODE, so
			// a read-back mode can legitimately differ from what you sent.
			"mode":         "MCP_SERVER_AUTH_OAUTH2_MODE_SERVICE",
			"clientId":     "<oauth-client-id>",
			"clientSecret": "<oauth-client-secret>",
			"authorizeUrl": "https://provider.example/oauth/authorize",
			"tokenUrl":     "https://provider.example/oauth/token",
			"scopes":       []string{"<scope>"},
		}}, nil // #nosec G101 -- placeholder template values (e.g. "<oauth-client-secret>"), not real secrets
	case "awsSigv4":
		return map[string]any{"awsSigv4": map[string]any{
			"accessKeyId":     "<aws-access-key-id>",
			"secretAccessKey": "<aws-secret-access-key>",
			"sessionToken":    "",
		}}, nil // #nosec G101 -- placeholder template values (e.g. "<aws-secret-access-key>"), not real secrets
	case "googleServiceAccount":
		return map[string]any{"googleServiceAccount": map[string]any{
			"credentialsJson": "<service-account-credentials-json-string>",
			"scopes":          []string{"<scope>"},
		}}, nil
	default:
		return nil, fmt.Errorf("--auth %q has no template", arm)
	}
}

// printConfigTemplate emits a ready-to-edit config skeleton for
// --hosted-config-file / --external-config-file to stdout (valid JSON, so it
// pipes to a file), with human guidance on stderr. It performs no auth and no
// network I/O.
func printConfigTemplate(cmd *cobra.Command) error {
	auth, _ := cmd.Flags().GetString("auth")

	// Bad --auth / --type are user-input problems: wrap as usageError so they
	// map to the documented usage exit code (2), not the generic exit code (1),
	// letting automation reliably distinguish misuse from real failures.
	arm, err := authArmTemplate(auth)
	if err != nil {
		return &usageError{err}
	}
	typ := "MCP_SERVER_TYPE_HOSTED"
	if t := mustGetString(cmd, "type"); t != "" {
		if typ, err = serverTypeFlag.parse(t); err != nil {
			return err
		}
	}
	// serverTypeFlag is closed, so typ is HOSTED or EXTERNAL.
	serverType := strings.ToLower(strings.TrimPrefix(typ, serverTypeFlag.prefix))

	cfg := map[string]any{}
	if typ == "MCP_SERVER_TYPE_EXTERNAL" {
		cfg["url"] = "https://your-mcp-server.example/mcp"
		cfg["transportType"] = "MCP_SERVER_TRANSPORT_TYPE_STREAMABLE_HTTP"
	} else {
		cfg["mcpServerCatalogId"] = "<catalog entry id — see: c1i mcp servers catalog list>"
		cfg["tokenSharing"] = "MCP_SERVER_TOKEN_SHARING_SHARED"
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

	flagName := serverType + "-config-file"
	authLabel := auth
	if authLabel == "" {
		authLabel = "none"
	}
	notice(cmd, noticeInfo, fmt.Sprintf(
		"# %s %s config template. Replace <placeholders>, then:\n#   c1i mcp servers register --app-id APP --type %s --display-name NAME --%s config.json\n# Fields with <angle-brackets> are placeholders; enum/URL values are examples. Secrets are sealed server-side.",
		serverType, authLabel, serverType, flagName), nil)
	// Not mutation output: this mode sends nothing.
	_, err = cmd.OutOrStdout().Write(buf.Bytes())
	return err
}

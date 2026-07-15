package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// addAuthFlags registers the convenience auth flags shared by register,
// update-credentials, and test-connection. They cover the simple auth methods;
// OAuth2 / AWS SigV4 / Google service-account configs go through the
// --hosted-config-file / --external-config-file JSON escape hatch instead.
func addAuthFlags(cmd *cobra.Command) {
	cmd.Flags().String("auth", "", "Auth method: none, bearer-token, custom-header, basic-auth (OAuth2/AWS/Google go via --*-config-file)")
	cmd.Flags().String("bearer-token", "", "Bearer token value (with --auth bearer-token)")
	cmd.Flags().String("header-name", "", "Custom header name (with --auth custom-header)")
	cmd.Flags().String("header-value", "", "Custom header value (with --auth custom-header)")
	cmd.Flags().String("basic-auth-username", "", "Basic-auth username (with --auth basic-auth)")
	cmd.Flags().String("basic-auth-password", "", "Basic-auth password (with --auth basic-auth)")
	cmd.Flags().String("token-sharing", "", "Token sharing: shared or per-user")
	cmd.Flags().Bool("require-tool-approval", false, "Per-server override for tool auto-approval (omit to leave unset)")
}

// authArmFromFlags builds the proto3-JSON oneof arm for the selected --auth
// method. Returns nil when --auth is unset so the caller can omit auth. oneof
// members serialize flat into the parent config object, so the returned map is
// meant to be merged into the hosted/external config.
func authArmFromFlags(cmd *cobra.Command) (map[string]any, error) {
	auth, _ := cmd.Flags().GetString("auth")
	switch strings.ToLower(auth) {
	case "":
		return nil, nil
	case "none":
		return map[string]any{"none": map[string]any{}}, nil
	case "bearer-token", "bearer":
		token, _ := cmd.Flags().GetString("bearer-token")
		return map[string]any{"bearerToken": map[string]any{"token": token}}, nil
	case "custom-header":
		name, _ := cmd.Flags().GetString("header-name")
		value, _ := cmd.Flags().GetString("header-value")
		return map[string]any{"customHeader": map[string]any{"headerName": name, "headerValue": value}}, nil
	case "basic-auth", "basic":
		user, _ := cmd.Flags().GetString("basic-auth-username")
		pass, _ := cmd.Flags().GetString("basic-auth-password")
		return map[string]any{"basicAuth": map[string]any{"username": user, "password": pass}}, nil
	default:
		return nil, fmt.Errorf("unsupported --auth %q: use none, bearer-token, custom-header, or basic-auth (OAuth2/AWS/Google go via --hosted-config-file / --external-config-file)", auth)
	}
}

// applySharedAuthFields merges the auth arm, token-sharing, and
// require-tool-approval flags into a config object built from flags.
func applySharedAuthFields(cmd *cobra.Command, cfg map[string]any) error {
	arm, err := authArmFromFlags(cmd)
	if err != nil {
		return err
	}
	for k, v := range arm {
		cfg[k] = v
	}
	if ts, _ := cmd.Flags().GetString("token-sharing"); ts != "" {
		cfg["tokenSharing"] = mapTokenSharing(ts)
	}
	if cmd.Flags().Changed("require-tool-approval") {
		on, _ := cmd.Flags().GetBool("require-tool-approval")
		if on {
			cfg["requireToolApproval"] = "OPTIONAL_BOOL_TRUE"
		} else {
			cfg["requireToolApproval"] = "OPTIONAL_BOOL_FALSE"
		}
	}
	return nil
}

// readConfigFile loads a JSON object from path (or stdin when path is "-") into
// a generic map, used as the full hosted/external config verbatim.
func readConfigFile(cmd *cobra.Command, path string) (map[string]any, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(cmd.InOrStdin())
	} else {
		data, err = os.ReadFile(path) //nolint:gosec // user-supplied config path is intentional
	}
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file as JSON object: %w", err)
	}
	return cfg, nil
}

// sharedAuthFlagsChanged reports whether any convenience auth flag (from
// addAuthFlags) was set. A config-file supplies the whole config verbatim, so
// these flags would be silently dropped alongside it — this lets the builders
// reject that combination instead of quietly discarding the values.
func sharedAuthFlagsChanged(cmd *cobra.Command) bool {
	for _, n := range []string{
		"auth", "bearer-token", "header-name", "header-value",
		"basic-auth-username", "basic-auth-password", "token-sharing", "require-tool-approval",
	} {
		if cmd.Flags().Changed(n) {
			return true
		}
	}
	return false
}

// buildHostedConfig assembles the hostedConfig object from --hosted-config-file
// (verbatim) or from the convenience flags (--catalog-id, --config-field, auth).
// The two are mutually exclusive.
func buildHostedConfig(cmd *cobra.Command) (map[string]any, error) {
	file, _ := cmd.Flags().GetString("hosted-config-file")
	usingFlags := cmd.Flags().Changed("catalog-id") || cmd.Flags().Changed("config-field") ||
		cmd.Flags().Changed("source-app-id") || sharedAuthFlagsChanged(cmd)
	if file != "" {
		if usingFlags {
			return nil, fmt.Errorf("--hosted-config-file is mutually exclusive with --catalog-id/--source-app-id/--config-field and the auth flags (--auth/--bearer-token/--token-sharing/…)")
		}
		return readConfigFile(cmd, file)
	}

	cfg := map[string]any{}
	if catalogID, _ := cmd.Flags().GetString("catalog-id"); catalogID != "" {
		cfg["mcpServerCatalogId"] = catalogID
	}
	if sourceAppID, _ := cmd.Flags().GetString("source-app-id"); sourceAppID != "" {
		cfg["sourceAppId"] = sourceAppID
	}
	fields, err := parseKeyValues(cmd, "config-field")
	if err != nil {
		return nil, err
	}
	if len(fields) > 0 {
		cfg["configFields"] = fields
	}
	if err := applySharedAuthFields(cmd, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// buildExternalConfig assembles the externalConfig object from
// --external-config-file (verbatim) or from the convenience flags (--url,
// --transport, auth). The two are mutually exclusive.
func buildExternalConfig(cmd *cobra.Command) (map[string]any, error) {
	file, _ := cmd.Flags().GetString("external-config-file")
	usingFlags := cmd.Flags().Changed("url") || cmd.Flags().Changed("transport") ||
		sharedAuthFlagsChanged(cmd)
	if file != "" {
		if usingFlags {
			return nil, fmt.Errorf("--external-config-file is mutually exclusive with --url/--transport and the auth flags (--auth/--bearer-token/--token-sharing/…)")
		}
		return readConfigFile(cmd, file)
	}

	cfg := map[string]any{}
	if url, _ := cmd.Flags().GetString("url"); url != "" {
		cfg["url"] = url
	}
	if transport, _ := cmd.Flags().GetString("transport"); transport != "" {
		cfg["transportType"] = mapTransportType(transport)
	}
	if err := applySharedAuthFields(cmd, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// parseKeyValues reads a repeatable "key=value" string-slice flag into a map.
func parseKeyValues(cmd *cobra.Command, name string) (map[string]string, error) {
	pairs, _ := cmd.Flags().GetStringSlice(name)
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid --%s %q: expected key=value", name, p)
		}
		out[k] = v
	}
	return out, nil
}

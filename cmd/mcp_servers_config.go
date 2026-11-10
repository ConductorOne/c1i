package cmd

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// deriveCredentialUpdateMask returns the update_mask for an UpdateCredentials
// request built from the convenience flags: the sorted set of top-level field
// paths present in cfg.
//
// The backend applies a credential/config change only when the mask names that
// field by its proto path — for the auth oneof that is the *case name* (e.g.
// "bearerToken", "none", "customHeader", "basicAuth", "oauth2"), NOT the
// wrapping "hostedConfig"/"externalConfig". The config builders already key the
// object by those exact paths, so the mask is simply cfg's keys. (Sending the
// wrapper name — the previous behavior — matched no path, so the backend
// silently dropped every credential change.) Mirrors the admin UI, which masks
// exactly the fields the user edited.
func deriveCredentialUpdateMask(cfg map[string]any) string {
	keys := make([]string, 0, len(cfg))
	for k := range cfg {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// addAuthFlags registers the convenience auth flags shared by register,
// update-credentials, and test-connection. They cover the simple auth methods;
// OAuth2 / AWS SigV4 / Google service-account configs go through the
// --hosted-config-file / --external-config-file JSON escape hatch instead.
func addAuthFlags(cmd *cobra.Command) {
	cmd.Flags().String("auth", "", "Auth method: "+authFlag.usage()+" (OAuth2/AWS/Google go via --*-config-file)")
	cmd.Flags().String("bearer-token", "", "Bearer token value (required with --auth bearer-token unless --token-sharing per-user or an explicit --update-mask omits it)")
	cmd.Flags().String("header-name", "", "Custom header name (required with --auth custom-header unless an explicit --update-mask omits it)")
	cmd.Flags().String("header-value", "", "Custom header value (required with --auth custom-header unless --token-sharing per-user or an explicit --update-mask omits it)")
	cmd.Flags().String("basic-auth-username", "", "Basic-auth username (required with --auth basic-auth unless --token-sharing per-user or an explicit --update-mask omits it)")
	cmd.Flags().String("basic-auth-password", "", "Basic-auth password (required with --auth basic-auth unless --token-sharing per-user or an explicit --update-mask omits it)")
	cmd.Flags().String("token-sharing", "", "Token sharing: "+tokenSharingFlag.usage())
	cmd.Flags().Bool("require-tool-approval", false, "Per-server override for tool auto-approval (omit to leave unset)")
}

// authArmFromFlags builds the proto3-JSON oneof arm for the selected --auth
// method. Returns nil when --auth is unset so the caller can omit auth. oneof
// members serialize flat into the parent config object, so the returned map is
// meant to be merged into the hosted/external config.
func authArmFromFlags(cmd *cobra.Command) (map[string]any, error) {
	auth := mustGetString(cmd, "auth")
	if auth == "" {
		return nil, nil
	}
	arm, err := authFlag.parse(auth)
	if err != nil {
		return nil, &usageError{fmt.Errorf("unsupported --auth %q: use %s (OAuth2/AWS/Google go via --hosted-config-file / --external-config-file)", auth, authFlag.usage())}
	}
	if err := requireAuthArmValues(cmd, arm); err != nil {
		return nil, err
	}
	switch arm {
	case "bearerToken":
		token, _ := cmd.Flags().GetString("bearer-token")
		return map[string]any{arm: map[string]any{keyToken: token}}, nil
	case "customHeader":
		name, _ := cmd.Flags().GetString("header-name")
		value, _ := cmd.Flags().GetString("header-value")
		return map[string]any{arm: map[string]any{"headerName": name, keyHeaderValue: value}}, nil
	case "basicAuth":
		user, _ := cmd.Flags().GetString("basic-auth-username")
		pass, _ := cmd.Flags().GetString("basic-auth-password")
		return map[string]any{arm: map[string]any{"username": user, keyPassword: pass}}, nil
	default: // none
		return map[string]any{arm: map[string]any{}}, nil
	}
}

// authArmField is one companion flag of an --auth arm and the mask paths that
// name it. Both spellings reach the server as the same FieldMask path: the API
// JSON form converts lowerCamelCase to proto snake_case.
type authArmField struct {
	flag  string
	paths []string
}

// authArmRule is an arm's companion flags and the coarse --update-mask path
// (its oneof case name) that asks for the whole arm.
type authArmRule struct {
	name   string
	coarse []string
	fields []authArmField
}

var authArmRules = map[string]authArmRule{
	"bearerToken": {
		name:   "bearer-token",
		coarse: []string{"bearerToken", "bearer_token"},
		fields: []authArmField{{"bearer-token", []string{"bearerToken.token", "bearer_token.token"}}},
	},
	"customHeader": {
		name:   "custom-header",
		coarse: []string{"customHeader", "custom_header"},
		fields: []authArmField{
			{"header-name", []string{"customHeader.headerName", "custom_header.header_name"}},
			{"header-value", []string{"customHeader.headerValue", "custom_header.header_value"}},
		},
	},
	"basicAuth": {
		name:   "basic-auth",
		coarse: []string{"basicAuth", "basic_auth"},
		fields: []authArmField{
			{"basic-auth-username", []string{"basicAuth.username", "basic_auth.username"}},
			{"basic-auth-password", []string{"basicAuth.password", "basic_auth.password"}},
		},
	},
}

// The auth-arm fields that hold a credential. The builders here and in the
// register template use these names, and isSecretKey (cmd/mutation.go) names the ones its
// suffix rule does not cover, so a preview masks what the builders write.
const (
	keyToken           = "token"
	keyHeaderValue     = "headerValue"
	keyPassword        = "password"
	keyClientSecret    = "clientSecret"
	keySecretAccessKey = "secretAccessKey"
	keySessionToken    = "sessionToken"
	keyCredentialsJSON = "credentialsJson"
)

// authArmPerUserOptional is the companions a PER_USER server may leave blank:
// each user supplies the secret later via SubmitUserCredential. A custom
// header's name is not one — the admin always sets it.
var authArmPerUserOptional = map[string]bool{
	"bearer-token":        true,
	"header-value":        true,
	"basic-auth-username": true,
	"basic-auth-password": true,
}

// requireAuthArmValues refuses a companion value the request would send as an
// empty credential. Two documented shapes are exempt, matching the server:
// PER_USER leaves the secret to SubmitUserCredential, and an explicit
// --update-mask requires only the fields it names (the server carries the
// rest forward), with a coarse arm path requiring all of them.
func requireAuthArmValues(cmd *cobra.Command, arm string) error {
	rule, ok := authArmRules[arm]
	if !ok { // none
		return nil
	}
	perUser := false
	if cmd.Flags().Lookup("token-sharing") != nil {
		ts, err := tokenSharingFlag.parse(mustGetString(cmd, "token-sharing"))
		if err != nil {
			return err
		}
		perUser = ts == "MCP_SERVER_TOKEN_SHARING_PER_USER"
	}

	required := rule.fields
	if mask, explicit := explicitUpdateMask(cmd); explicit {
		paths := maskPaths(mask)
		if !maskNamesAny(paths, rule.coarse) {
			required = nil
			for _, f := range rule.fields {
				if maskNamesAny(paths, f.paths) {
					required = append(required, f)
				}
			}
		}
	}

	var missing []string
	for _, f := range required {
		if perUser && authArmPerUserOptional[f.flag] {
			continue
		}
		if v, _ := cmd.Flags().GetString(f.flag); !hasVisibleRune(v) {
			missing = append(missing, "--"+f.flag)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	requiredText := strings.Join(missing, ", ")
	if len(missing) > 1 {
		requiredText = strings.Join(missing[:len(missing)-1], ", ") + " and " + missing[len(missing)-1]
	}
	return &usageError{fmt.Errorf("--auth %s requires %s", rule.name, requiredText)}
}

// explicitUpdateMask returns the caller's non-empty --update-mask when the
// command will actually send it. An empty mask means "derive it from the
// flags", i.e. the supplied arm is a full replacement. test-connection sends
// the mask only in edit mode (which requires --app-id); a create-mode probe
// ignores it, so it must not relax the full-arm requirement.
func explicitUpdateMask(cmd *cobra.Command) (string, bool) {
	if cmd.Flags().Lookup("update-mask") == nil {
		return "", false
	}
	mask, _ := cmd.Flags().GetString("update-mask")
	if mask == "" {
		return "", false
	}
	// update-credentials (which has --type) always sends the mask;
	// test-connection (no --type) sends it only in edit mode, which requires
	// --app-id, and ignores it in create mode.
	if cmd.Flags().Lookup("type") == nil {
		if appID, _ := cmd.Flags().GetString("app-id"); !hasVisibleRune(appID) {
			return "", false
		}
	}
	return mask, true
}

// maskPaths splits a comma-separated update mask, dropping blanks.
func maskPaths(mask string) []string {
	var paths []string
	for _, p := range strings.Split(mask, ",") {
		if p = strings.TrimSpace(p); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

func maskNamesAny(paths, names []string) bool {
	for _, p := range paths {
		if slices.Contains(names, p) {
			return true
		}
	}
	return false
}

// authFlag lists the --auth methods the convenience flags can fill in; api
// is the config's oneof arm.
var authFlag = enumFlag{flag: "auth", unknown: enumReject, values: []enumValue{
	{name: "none", api: "none"},
	{name: "bearer-token", api: "bearerToken", aliases: []string{"bearer"}},
	{name: "custom-header", api: "customHeader"},
	{name: "basic-auth", api: "basicAuth", aliases: []string{"basic"}},
}}

// authTemplateFlag adds the methods only a config file can carry, for
// --print-config-template.
var authTemplateFlag = enumFlag{flag: "auth", unknown: enumReject, values: append(slices.Clone(authFlag.values),
	enumValue{name: "oauth2", api: "oauth2"},
	enumValue{name: "aws-sigv4", api: "awsSigv4", aliases: []string{"aws"}},
	enumValue{name: "google-service-account", api: "googleServiceAccount", aliases: []string{"google"}},
)}

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
	if ts, err := tokenSharingFlag.parse(mustGetString(cmd, "token-sharing")); err != nil {
		return err
	} else if ts != "" {
		cfg["tokenSharing"] = ts
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
			return nil, &usageError{fmt.Errorf("--hosted-config-file is mutually exclusive with --catalog-id/--source-app-id/--config-field and the auth flags (--auth/--bearer-token/--token-sharing/…)")}
		}
		return readJSONFile[map[string]any](cmd, "hosted-config-file", file)
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
// --external-config-file (verbatim) or from the convenience flags (--server-url,
// --transport, auth). The two are mutually exclusive.
func buildExternalConfig(cmd *cobra.Command) (map[string]any, error) {
	file, _ := cmd.Flags().GetString("external-config-file")
	usingFlags := cmd.Flags().Changed("server-url") || cmd.Flags().Changed("transport") ||
		sharedAuthFlagsChanged(cmd)
	if file != "" {
		if usingFlags {
			return nil, &usageError{fmt.Errorf("--external-config-file is mutually exclusive with --server-url/--transport and the auth flags (--auth/--bearer-token/--token-sharing/…)")}
		}
		return readJSONFile[map[string]any](cmd, "external-config-file", file)
	}

	cfg := map[string]any{}
	if url, _ := cmd.Flags().GetString("server-url"); url != "" {
		cfg["url"] = url
	}
	if transport, err := transportTypeFlag.parse(mustGetString(cmd, "transport")); err != nil {
		return nil, err
	} else if transport != "" {
		cfg["transportType"] = transport
	}
	if err := applySharedAuthFields(cmd, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// parseKeyValues reads a repeatable "key=value" flag into a map.
func parseKeyValues(cmd *cobra.Command, name string) (map[string]string, error) {
	pairs, err := repeatableStringFlag(cmd, name)
	if err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, &usageError{fmt.Errorf("invalid --%s %q: expected key=value", name, p)}
		}
		out[k] = v
	}
	return out, nil
}

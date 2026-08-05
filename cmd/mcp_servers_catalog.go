package cmd

import (
	"strconv"

	"github.com/spf13/cobra"
)

var mcpServersCatalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "Browse the HOSTED MCP server catalog",
	Long: `List and inspect the catalog of HOSTED MCP server templates an admin
picks from when registering. Each entry carries a catalog_id (pass it to
"c1i mcp servers register --catalog-id"), the impl service_name, and the
supported auth methods / extra config schema.

Many well-known services appear TWICE in the catalog: once as a thin wrapper
over the vendor's plain REST API (display_name typically ends in "API",
service_name has no suffix, e.g. "slack") and once as the vendor's own hosted
MCP endpoint (service_name typically ends in "-mcp", base_url points at an
"mcp." host, e.g. "slack-mcp"). "c1i mcp servers catalog list" surfaces
service_name, base_url, default_tool_prefix, and stable specifically so these
near-duplicates (Slack vs "Slack API", Notion vs "Notion API", etc.) can be
told apart without a round trip to "get".

Scope tiering: required vs. optional OAuth scopes exist in the API, but per
auth mode, not as a single catalog-wide list — see each authMode's "scopes"
(required) vs "optionalScopes" (grants extra tools/permissions) in
"catalog get". The list row's required_scope_count/optional_scope_count
summarize those two sets (deduped across all of an entry's auth modes) so you
can spot at a glance which entries have an optional tier at all. The
entry-level "defaultScopes" field exists in the schema but was empty on every
entry observed in production catalog data — it is not currently a source of
scope tiering.`,
}

func init() {
	mcpServersCmd.AddCommand(mcpServersCatalogCmd)
}

// catalogAuthModeScopes is the subset of an MCPServerCatalogAuthMode needed to
// summarize scope tiering: "scopes" is that auth mode's required set,
// "optionalScopes" is the additional set that unlocks more tools/permissions
// but isn't needed to register. Both are real, present-in-the-API fields;
// they are per-auth-mode, not a single catalog-entry-wide tier.
type catalogAuthModeScopes struct {
	Scopes         []string `json:"scopes"`
	OptionalScopes []string `json:"optionalScopes"`
}

// catalogEntryView is the subset of MCPServerCatalogEntry surfaced in list
// rows. BaseURL, DefaultToolPrefix, and Stable are the fields that actually
// distinguish the catalog's many near-duplicate entries (e.g. "Slack" vs
// "Slack API") from each other; ServiceName already did some of that work but
// is easy to miss buried among other fields. AuthModes/DefaultScopes are not
// emitted verbatim in the row (too large for NDJSON) but are summarized by
// catalogEntryRow into required/optional scope counts.
type catalogEntryView struct {
	ID                string                  `json:"id"`
	DisplayName       string                  `json:"displayName"`
	Description       string                  `json:"description"`
	ServiceName       string                  `json:"serviceName"`
	BaseURL           string                  `json:"baseUrl"`
	DefaultToolPrefix string                  `json:"defaultToolPrefix"`
	Channel           string                  `json:"channel"`
	Scope             string                  `json:"scope"`
	Maturity          string                  `json:"maturity"`
	Stable            bool                    `json:"stable"`
	DefaultScopes     []string                `json:"defaultScopes"`
	AuthModes         []catalogAuthModeScopes `json:"authModes"`
}

// catalogScopeCounts returns the number of distinct required scopes and
// distinct optional scopes across all of an entry's auth modes (a scope
// appearing in more than one auth mode is only counted once). Different auth
// modes on the same entry can carry different scope sets (e.g. Slack's OAuth2
// mode has 27 optional scopes; its bearer-token mode has none), so this is a
// union across modes, not a single mode's counts.
func catalogScopeCounts(modes []catalogAuthModeScopes) (required, optional int) {
	reqSet := make(map[string]struct{})
	optSet := make(map[string]struct{})
	for _, m := range modes {
		for _, s := range m.Scopes {
			reqSet[s] = struct{}{}
		}
		for _, s := range m.OptionalScopes {
			optSet[s] = struct{}{}
		}
	}
	return len(reqSet), len(optSet)
}

func catalogEntryRow(e catalogEntryView) map[string]string {
	required, optional := catalogScopeCounts(e.AuthModes)
	return map[string]string{
		"id":                   e.ID,
		"display_name":         e.DisplayName,
		"description":          e.Description,
		"service_name":         e.ServiceName,
		"base_url":             e.BaseURL,
		"default_tool_prefix":  e.DefaultToolPrefix,
		"channel":              e.Channel,
		"scope":                e.Scope,
		"maturity":             e.Maturity,
		"stable":               strconv.FormatBool(e.Stable),
		"required_scope_count": strconv.Itoa(required),
		"optional_scope_count": strconv.Itoa(optional),
	}
}

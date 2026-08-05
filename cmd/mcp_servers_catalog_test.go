package cmd

import (
	"encoding/json"
	"testing"
)

// TestCatalogEntryRowDisambiguation exercises the two real, observed catalog
// entries for Slack (fetched from a live tenant during development) to make
// sure the row surfaces the fields that actually distinguish a REST-wrapper
// entry from its hosted-MCP near-duplicate: base_url, default_tool_prefix,
// stable, and service_name all differ even though display_name alone
// ("Slack API" vs "Slack") is easy to conflate.
func TestCatalogEntryRowDisambiguation(t *testing.T) {
	restWrapper := `{
		"id": "3Aaj3lKAbL5Dft3DAOVyN0ltRAp",
		"displayName": "Slack API",
		"description": "Slack API for managing channels, messages, users, user groups, and files across your Slack workspace.",
		"serviceName": "slack",
		"baseUrl": "https://slack.com/api",
		"defaultToolPrefix": "slack_api",
		"channel": "MCP_SERVER_CATALOG_CHANNEL_STABLE",
		"scope": "MCP_SERVER_CATALOG_SCOPE_BUSINESS",
		"maturity": "MCP_SERVER_CATALOG_MATURITY_CURATED",
		"stable": true,
		"defaultScopes": [],
		"authModes": [{
			"scopes": ["channels:read", "channels:history", "groups:read", "groups:history", "im:read", "im:history", "mpim:read", "mpim:history", "users:read", "users:read.email", "usergroups:read", "files:read", "pins:read", "search:read"],
			"optionalScopes": ["chat:write", "reactions:read", "reminders:read", "stars:read", "remote_files:read", "remote_files:share", "users.profile:write", "users:write", "channels:write", "groups:write", "im:write", "mpim:write", "channels:write.invites", "groups:write.invites", "channels:write.topic", "groups:write.topic", "im:write.topic", "mpim:write.topic", "dnd:read", "calls:read", "admin.conversations:read", "admin.conversations:write", "admin.teams:read", "admin.teams:write", "admin.invites:read", "admin.users:read", "admin.users:write"]
		}]
	}`
	hostedMCP := `{
		"id": "3GBynv4ntUVsVWoWFZxGFt2xC5s",
		"displayName": "Slack",
		"description": "Slack MCP for searching, reading, and managing messages, channels, canvases, and reactions across your Slack workspace.",
		"serviceName": "slack-mcp",
		"baseUrl": "https://mcp.slack.com/mcp",
		"defaultToolPrefix": "slack",
		"channel": "MCP_SERVER_CATALOG_CHANNEL_STABLE",
		"scope": "MCP_SERVER_CATALOG_SCOPE_BUSINESS",
		"maturity": "MCP_SERVER_CATALOG_MATURITY_GENERATED",
		"stable": true,
		"defaultScopes": [],
		"authModes": [
			{
				"scopes": ["search:read.public", "search:read.private", "search:read.mpim", "search:read.im", "search:read.files", "search:read.users", "chat:write", "channels:history", "groups:history", "mpim:history", "im:history", "canvases:read", "canvases:write", "users:read", "users:read.email", "reactions:write", "reactions:read", "emoji:read", "files:read", "channels:write", "groups:write", "im:write", "mpim:write", "channels:read", "groups:read", "mpim:read"],
				"optionalScopes": []
			},
			{
				"scopes": [],
				"optionalScopes": []
			}
		]
	}`

	var rest, hosted catalogEntryView
	if err := json.Unmarshal([]byte(restWrapper), &rest); err != nil {
		t.Fatalf("unmarshal restWrapper: %v", err)
	}
	if err := json.Unmarshal([]byte(hostedMCP), &hosted); err != nil {
		t.Fatalf("unmarshal hostedMCP: %v", err)
	}

	restRow := catalogEntryRow(rest)
	hostedRow := catalogEntryRow(hosted)

	wantRest := map[string]string{
		"id":                   "3Aaj3lKAbL5Dft3DAOVyN0ltRAp",
		"display_name":         "Slack API",
		"service_name":         "slack",
		"base_url":             "https://slack.com/api",
		"default_tool_prefix":  "slack_api",
		"maturity":             "MCP_SERVER_CATALOG_MATURITY_CURATED",
		"stable":               "true",
		"required_scope_count": "14",
		"optional_scope_count": "27",
	}
	for k, want := range wantRest {
		if got := restRow[k]; got != want {
			t.Errorf("restRow[%q] = %q, want %q", k, got, want)
		}
	}

	wantHosted := map[string]string{
		"id":                   "3GBynv4ntUVsVWoWFZxGFt2xC5s",
		"display_name":         "Slack",
		"service_name":         "slack-mcp",
		"base_url":             "https://mcp.slack.com/mcp",
		"default_tool_prefix":  "slack",
		"maturity":             "MCP_SERVER_CATALOG_MATURITY_GENERATED",
		"stable":               "true",
		"required_scope_count": "26",
		"optional_scope_count": "0",
	}
	for k, want := range wantHosted {
		if got := hostedRow[k]; got != want {
			t.Errorf("hostedRow[%q] = %q, want %q", k, got, want)
		}
	}

	// The whole point: every disambiguating field must actually differ between
	// the two entries, or an agent picking a catalog_id from list output alone
	// would have no way to tell them apart.
	disambiguators := []string{"id", "service_name", "base_url", "default_tool_prefix", "maturity"}
	for _, k := range disambiguators {
		if restRow[k] == hostedRow[k] {
			t.Errorf("field %q is identical between the two Slack entries (%q); it fails to disambiguate them", k, restRow[k])
		}
	}
}

// TestCatalogScopeCounts covers the scope-tiering summary in isolation,
// including entries with no auth modes, multiple auth modes with different
// scope sets, and scopes that overlap across modes (which must be deduped,
// not double-counted).
func TestCatalogScopeCounts(t *testing.T) {
	cases := []struct {
		name         string
		modes        []catalogAuthModeScopes
		wantRequired int
		wantOptional int
	}{
		{
			name:         "no auth modes",
			modes:        nil,
			wantRequired: 0,
			wantOptional: 0,
		},
		{
			name: "single mode, no optional tier",
			modes: []catalogAuthModeScopes{
				{Scopes: []string{"a", "b"}, OptionalScopes: nil},
			},
			wantRequired: 2,
			wantOptional: 0,
		},
		{
			name: "single mode with optional tier",
			modes: []catalogAuthModeScopes{
				{Scopes: []string{"a", "b"}, OptionalScopes: []string{"c", "d", "e"}},
			},
			wantRequired: 2,
			wantOptional: 3,
		},
		{
			name: "multiple modes, overlapping scopes deduped",
			modes: []catalogAuthModeScopes{
				{Scopes: []string{"a", "b"}, OptionalScopes: []string{"x"}},
				{Scopes: []string{"b", "c"}, OptionalScopes: []string{"x", "y"}},
			},
			wantRequired: 3, // a, b, c
			wantOptional: 2, // x, y
		},
		{
			// No production entry does this today, but the API gives no
			// guarantee it never will: a scope required by one auth mode
			// and merely optional in another must still count as required
			// only — the two tiers must stay disjoint.
			name: "scope required in one mode, optional in another",
			modes: []catalogAuthModeScopes{
				{Scopes: []string{"a"}, OptionalScopes: nil},
				{Scopes: []string{"b"}, OptionalScopes: []string{"a", "c"}},
			},
			wantRequired: 2, // a, b
			wantOptional: 1, // c (not a, since a is required elsewhere)
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotRequired, gotOptional := catalogScopeCounts(c.modes)
			if gotRequired != c.wantRequired {
				t.Errorf("required = %d, want %d", gotRequired, c.wantRequired)
			}
			if gotOptional != c.wantOptional {
				t.Errorf("optional = %d, want %d", gotOptional, c.wantOptional)
			}
		})
	}
}

// TestCatalogEntryRowSuperset locks in that every field the previous row
// implementation emitted (id, display_name, description, service_name,
// channel, scope, maturity) is still present, so --fields specs written
// against the old shape keep working.
func TestCatalogEntryRowSuperset(t *testing.T) {
	raw := `{
		"id": "cat1",
		"displayName": "Example",
		"description": "An example entry",
		"serviceName": "example",
		"baseUrl": "https://api.example.com",
		"defaultToolPrefix": "example",
		"channel": "MCP_SERVER_CATALOG_CHANNEL_ALPHA",
		"scope": "MCP_SERVER_CATALOG_SCOPE_BUSINESS",
		"maturity": "MCP_SERVER_CATALOG_MATURITY_GENERATED",
		"stable": false,
		"defaultScopes": [],
		"authModes": []
	}`
	var v catalogEntryView
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	row := catalogEntryRow(v)
	preExisting := map[string]string{
		"id":           "cat1",
		"display_name": "Example",
		"description":  "An example entry",
		"service_name": "example",
		"channel":      "MCP_SERVER_CATALOG_CHANNEL_ALPHA",
		"scope":        "MCP_SERVER_CATALOG_SCOPE_BUSINESS",
		"maturity":     "MCP_SERVER_CATALOG_MATURITY_GENERATED",
	}
	for k, want := range preExisting {
		if got, ok := row[k]; !ok || got != want {
			t.Errorf("row[%q] = %q (present=%v), want %q", k, got, ok, want)
		}
	}
	// And the new fields are present with sane defaults on an entry with no
	// scope tiering at all.
	newFields := map[string]string{
		"base_url":             "https://api.example.com",
		"default_tool_prefix":  "example",
		"stable":               "false",
		"required_scope_count": "0",
		"optional_scope_count": "0",
	}
	for k, want := range newFields {
		if got := row[k]; got != want {
			t.Errorf("row[%q] = %q, want %q", k, got, want)
		}
	}
}

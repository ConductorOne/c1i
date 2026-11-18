package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpResourcesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List MCP resources discovered for a connector (NDJSON output)",
	Long: `List every MCP resource stored for a connector, in every state including
removed (sync marks a resource removed when the upstream server stops
exposing it). Use "mcp resources search" to filter by state or kind.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, connectorID := scopeIDs(cmd)
		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_resources", appID, connectorID)
		return listPages(cmd, c, pageSpec[resourceView]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseKeyed[resourceView]("resources"),
			ID:    func(it resourceView) string { return it.ID },
			Rows:  rowFn(func(r resourceView) map[string]any { return resourceRow(r, false) }),
		})
	},
}

var mcpResourcesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search MCP resources with filters (NDJSON output)",
	Long: `Search a connector's MCP resources by text, state and kind.

Without --state the server returns pending, approved and disabled resources;
removed ones appear only with --state removed. --query matches name, title,
description and URI. --include-grant-status adds entitlement_active and
grant_count to each row; without it those keys are omitted, because the
server does not compute them.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		states, err := parseRepeatableEnum(cmd, resourceStateFlag)
		if err != nil {
			return err
		}
		kinds, err := parseRepeatableEnum(cmd, resourceKindFlag)
		if err != nil {
			return err
		}
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, connectorID := scopeIDs(cmd)
		body, grants := itemSearchBody(cmd, appID, connectorID, states)
		if len(kinds) > 0 {
			body["kindFilter"] = kinds
		}
		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_resources/search", appID, connectorID)
		return listPages(cmd, c, pageSpec[resourceView]{
			Route: "POST " + path,
			Fetch: postPage(cmd.Context(), path, body),
			Parse: parseList[resourceView],
			ID:    func(it resourceView) string { return it.ID },
			Rows:  rowFn(func(r resourceView) map[string]any { return resourceRow(r, grants) }),
		})
	},
}

// itemSearchBody is the request body prompts and resources search share. It
// reports whether grant status was asked for, since only then are the grant
// fields computed.
func itemSearchBody(cmd *cobra.Command, appID, connectorID string, states []string) (map[string]any, bool) {
	body := map[string]any{"appId": appID, "connectorId": connectorID}
	if query := mustGetString(cmd, "query"); query != "" {
		body["query"] = query
	}
	if len(states) > 0 {
		body["stateFilter"] = states
	}
	grants, _ := cmd.Flags().GetBool("include-grant-status")
	if grants {
		body["includeGrantStatus"] = true
	}
	return body, grants
}

// grantStatus is the pair Search computes only with includeGrantStatus.
type grantStatus struct {
	EntitlementActive bool      `json:"entitlementActive"`
	GrantCount        flexInt64 `json:"grantCount"`
}

// addTo sets the grant columns when they were computed; otherwise the keys
// stay absent, so an uncomputed false/0 is never mistaken for a real one.
func (g grantStatus) addTo(row map[string]any, computed bool) map[string]any {
	if computed {
		row["entitlement_active"] = g.EntitlementActive
		row["grant_count"] = int64(g.GrantCount)
	}
	return row
}

type resourceView struct {
	ID               string `json:"id"`
	AppID            string `json:"appId"`
	ConnectorID      string `json:"connectorId"`
	Name             string `json:"name"`
	Title            string `json:"title"`
	Description      string `json:"description"`
	Kind             string `json:"kind"`
	URI              string `json:"uri"`
	URITemplate      string `json:"uriTemplate"`
	MimeType         string `json:"mimeType"`
	State            string `json:"state"`
	AppEntitlementID string `json:"appEntitlementId"`
	DeletedAt        string `json:"deletedAt"`
	grantStatus
}

func resourceRow(r resourceView, grants bool) map[string]any {
	return r.addTo(map[string]any{
		"id":                 nilIfEmpty(r.ID),
		"app_id":             nilIfEmpty(r.AppID),
		"connector_id":       nilIfEmpty(r.ConnectorID),
		"name":               nilIfEmpty(r.Name),
		"title":              nilIfEmpty(r.Title),
		"description":        nilIfEmpty(r.Description),
		"kind":               nilIfEmpty(r.Kind),
		"uri":                nilIfEmpty(r.URI),
		"uri_template":       nilIfEmpty(r.URITemplate),
		"mime_type":          nilIfEmpty(r.MimeType),
		"state":              nilIfEmpty(r.State),
		"app_entitlement_id": nilIfEmpty(r.AppEntitlementID),
		"deleted_at":         nilIfEmpty(r.DeletedAt),
	}, grants)
}

// addItemSearchFlags registers the scope, --query, --state and grant flags
// prompts and resources search share.
func addItemSearchFlags(c *cobra.Command, noun, queryFields string, state enumFlag) {
	addMCPScopeFlags(c)
	c.Flags().String("query", "", "Text matched against "+queryFields)
	addRepeatableStringFlag(c, "state", "Filter by state (repeatable; default pending, approved and disabled): "+state.usage())
	c.Flags().Bool("include-grant-status", false, "Add entitlement_active and grant_count to each "+noun+" row")
	addPaginationFlags(c)
	markRequired(c, "app-id", "connector-id")
}

func init() {
	addMCPScopeFlags(mcpResourcesListCmd)
	addPaginationFlags(mcpResourcesListCmd)
	markRequired(mcpResourcesListCmd, "app-id", "connector-id")
	mcpResourcesCmd.AddCommand(mcpResourcesListCmd)

	addItemSearchFlags(mcpResourcesSearchCmd, "resource", "name, title, description and URI", resourceStateFlag)
	addRepeatableStringFlag(mcpResourcesSearchCmd, "kind", "Filter by kind (repeatable): "+resourceKindFlag.usage())
	mcpResourcesCmd.AddCommand(mcpResourcesSearchCmd)
}

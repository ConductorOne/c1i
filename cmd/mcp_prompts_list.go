package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpPromptsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List MCP prompts stored for a connector (NDJSON output)",
	Long: `List the MCP prompts stored for a connector. Use "mcp prompts search" to
filter by state or text.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, connectorID := scopeIDs(cmd)
		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_prompts", appID, connectorID)
		return listPages(cmd, c, pageSpec[promptView]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseKeyed[promptView]("prompts"),
			ID:    func(it promptView) string { return it.ID },
			Rows:  rowFn(func(p promptView) map[string]any { return promptRow(p, false) }),
		})
	},
}

var mcpPromptsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search MCP prompts with filters (NDJSON output)",
	Long: `Search a connector's MCP prompts by text and state.

The API spec says an empty state filter "selects PENDING_REVIEW, APPROVED and
DISABLED; request REMOVED explicitly", so removed prompts appear only with
--state removed. --include-grant-status adds entitlement_active and
grant_count to each row; without it those keys are omitted, because the
server does not compute them.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		states, err := parseRepeatableEnum(cmd, promptStateFlag)
		if err != nil {
			return err
		}
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, connectorID := scopeIDs(cmd)
		body, grants := itemSearchBody(cmd, appID, connectorID, states)
		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_prompts/search", appID, connectorID)
		return listPages(cmd, c, pageSpec[promptView]{
			Route: "POST " + path,
			Fetch: postPage(cmd.Context(), path, body),
			Parse: parseList[promptView],
			ID:    func(it promptView) string { return it.ID },
			Rows:  rowFn(func(p promptView) map[string]any { return promptRow(p, grants) }),
		})
	},
}

type promptView struct {
	ID               string `json:"id"`
	AppID            string `json:"appId"`
	ConnectorID      string `json:"connectorId"`
	Name             string `json:"name"`
	Title            string `json:"title"`
	Description      string `json:"description"`
	State            string `json:"state"`
	AppEntitlementID string `json:"appEntitlementId"`
	DeletedAt        string `json:"deletedAt"`
	Arguments        []struct {
		Name string `json:"name"`
	} `json:"arguments"`
	grantStatus
}

// promptRow lists argument names only; "mcp prompts get" has the full
// definitions. argument_names is always an array, [] when there are none.
func promptRow(p promptView, grants bool) map[string]any {
	names := make([]string, 0, len(p.Arguments))
	for _, a := range p.Arguments {
		names = append(names, a.Name)
	}
	return p.addTo(map[string]any{
		"id":                 nilIfEmpty(p.ID),
		"app_id":             nilIfEmpty(p.AppID),
		"connector_id":       nilIfEmpty(p.ConnectorID),
		"name":               nilIfEmpty(p.Name),
		"title":              nilIfEmpty(p.Title),
		"description":        nilIfEmpty(p.Description),
		"state":              nilIfEmpty(p.State),
		"app_entitlement_id": nilIfEmpty(p.AppEntitlementID),
		"argument_names":     names,
		"deleted_at":         nilIfEmpty(p.DeletedAt),
	}, grants)
}

func init() {
	addMCPScopeFlags(mcpPromptsListCmd)
	addPaginationFlags(mcpPromptsListCmd)
	markRequired(mcpPromptsListCmd, "app-id", "connector-id")
	mcpPromptsCmd.AddCommand(mcpPromptsListCmd)

	addItemSearchFlags(mcpPromptsSearchCmd, "prompt", "name, title and description", promptStateFlag)
	mcpPromptsCmd.AddCommand(mcpPromptsSearchCmd)
}

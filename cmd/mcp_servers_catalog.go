package cmd

import "github.com/spf13/cobra"

var mcpServersCatalogCmd = &cobra.Command{
	Use:   "catalog",
	Short: "Browse the HOSTED MCP server catalog",
	Long: `List and inspect the catalog of HOSTED MCP server templates an admin
picks from when registering. Each entry carries a catalog_id (pass it to
"c1i mcp servers register --catalog-id"), the impl service_name, and the
supported auth methods / extra config schema.`,
}

func init() {
	mcpServersCmd.AddCommand(mcpServersCatalogCmd)
}

// catalogEntryView is the subset of MCPServerCatalogEntry surfaced in list rows.
type catalogEntryView struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	ServiceName string `json:"serviceName"`
	Channel     string `json:"channel"`
	Scope       string `json:"scope"`
	Maturity    string `json:"maturity"`
}

func catalogEntryRow(e catalogEntryView) map[string]string {
	return map[string]string{
		"id":           e.ID,
		"display_name": e.DisplayName,
		"description":  e.Description,
		"service_name": e.ServiceName,
		"channel":      e.Channel,
		"scope":        e.Scope,
		"maturity":     e.Maturity,
	}
}

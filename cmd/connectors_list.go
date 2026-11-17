package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// connectorListItem is one row of GET /api/v1/apps/{appId}/connectors.
type connectorListItem struct {
	Connector struct {
		ID          string `json:"id"`
		AppID       string `json:"appId"`
		DisplayName string `json:"displayName"`
		Status      struct {
			Status string `json:"status"`
		} `json:"status"`
		DeletedAt string `json:"deletedAt"`
	} `json:"connector"`
}

// connectorRow flattens a connectorListItem into the NDJSON output row.
// deleted_at is nil, not "", on a live connector.
func connectorRow(item connectorListItem) map[string]any {
	conn := item.Connector
	return map[string]any{
		"id":           nilIfEmpty(conn.ID),
		"app_id":       nilIfEmpty(conn.AppID),
		"display_name": nilIfEmpty(conn.DisplayName),
		"status":       nilIfEmpty(conn.Status.Status),
		"deleted_at":   nilIfEmpty(conn.DeletedAt),
	}
}

var connectorsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List connectors for an application (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {

		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		path := client.Path("/api/v1/apps/%s/connectors", appID)
		return listPages(cmd, c, pageSpec[connectorListItem]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseList[connectorListItem],
			ID:    func(it connectorListItem) string { return it.Connector.ID },
			Rows:  rowFn(connectorRow),
		})
	},
}

func init() {
	connectorsListCmd.Flags().String("app-id", "", "Application ID")
	addPaginationFlags(connectorsListCmd)
	markRequired(connectorsListCmd, "app-id")
	connectorsCmd.AddCommand(connectorsListCmd)
}

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var appsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new app (pretty JSON)",
	Long: `Create a new app (a container you can register MCP servers under).

Only --display-name is required. This creates a plain, unmanaged container
app — the zero state for "make an app, then register MCP servers under it".
The caller is auto-assigned as an owner of the new app, appearing in
"apps owners" after the usual owner-provisioning lag. Change the list with
"apps add-owner"/"apps remove-owner", or "apps set-owners", which replaces
the whole list -- including you.

The created app is returned as pretty JSON under an "app" key (--fields is not
applied to mutation output, so parse the id from the full object).

Example:
  APP_ID=$(c1i apps create --display-name "Google Workspace" | jq -r .app.id)
  c1i mcp servers register --app-id "$APP_ID" --type hosted ...`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "display-name"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		body := buildAppCreateBody(cmd)

		if dryRunActive() {
			return printDryRun(cmd, "POST", "/api/v1/apps", body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), "/api/v1/apps", body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

// buildAppCreateBody assembles the CreateApp request body from flags. Pure (no
// network / auth) so the dry-run preview and unit tests exercise the same body
// the live request sends. Optional fields are omitted when empty rather than
// sent as empty strings/arrays.
func buildAppCreateBody(cmd *cobra.Command) map[string]any {
	displayName, _ := cmd.Flags().GetString("display-name")
	body := map[string]any{"displayName": displayName}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	return body
}

func init() {
	appsCreateCmd.Flags().String("display-name", "", "Display name for the new app")
	appsCreateCmd.Flags().String("description", "", "Description for the new app")
	markRequired(appsCreateCmd, "display-name")
	appsCmd.AddCommand(appsCreateCmd)
}

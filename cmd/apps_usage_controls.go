package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsUsageControlsCmd = &cobra.Command{
	Use:   "usage-controls",
	Short: "Show an app's usage controls",
	Long: `Show an app's usage controls: whether to notify, and whether to revoke,
users who hold access to the app but have not used it within a number of days.`,
}

func usageControlsPath(appID string) string {
	return client.Path("/api/v1/apps/%s/usage_controls", appID)
}

// dropNullUsageControls removes a null appUsageControls, so an app with no
// controls prints only hasUsageData rather than a key that is null on some
// apps and an object on others.
func dropNullUsageControls(data []byte) []byte {
	var resp map[string]json.RawMessage
	if json.Unmarshal(data, &resp) != nil {
		return data
	}
	v, ok := resp["appUsageControls"]
	if !ok || !bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
		return data
	}
	delete(resp, "appUsageControls")
	out, err := json.Marshal(resp)
	if err != nil {
		return data
	}
	return out
}

var appsUsageControlsGetCmd = &cobra.Command{
	Use:   "get <app-id>",
	Short: "Show an app's usage controls (pretty JSON)",
	Long: `Show an app's usage controls (pretty JSON), unwrapped: appId, notify,
notifyAfterDays, revoke and revokeAfterDays at the top level, beside
hasUsageData (false when the app's access entitlement has no usage data).
An app whose controls were never set prints only hasUsageData.

The usage-controls endpoint answers an unknown app as it does an app with no
controls, so c1i reads the app first: an unknown app exits 4.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		if _, err := c.Get(cmd.Context(), client.Path("/api/v1/apps/%s", args[0]), nil); err != nil {
			return fmt.Errorf("API error reading app %s: %w", args[0], err)
		}
		data, err := c.Get(cmd.Context(), usageControlsPath(args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeNamedResource(cmd, dropNullUsageControls(data), "appUsageControls")
	},
}

func init() {
	appsUsageControlsCmd.AddCommand(appsUsageControlsGetCmd)
	appsCmd.AddCommand(appsUsageControlsCmd)
}

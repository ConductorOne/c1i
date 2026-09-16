package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var servicePrincipalsUpdateCmd = &cobra.Command{
	Use:   "update <sp-id>",
	Short: "Update a service principal's display name",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		sp := map[string]any{"id": id}
		var paths []string
		if cmd.Flags().Changed("display-name") {
			v, _ := cmd.Flags().GetString("display-name")
			sp["displayName"] = v
			paths = append(paths, "displayName")
		}
		if len(paths) == 0 {
			return &usageError{fmt.Errorf("nothing to update: pass --display-name")}
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		body := map[string]any{
			"servicePrincipal": sp,
			"updateMask":       strings.Join(paths, ","),
		}
		path := spPath(id)
		if dryRunActive() {
			return printDryRun(cmd, "PATCH", path, body)
		}

		c, err := newServicePrincipalsClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Patch(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

func init() {
	servicePrincipalsUpdateCmd.Flags().String("display-name", "", "New display name")
	servicePrincipalsCmd.AddCommand(servicePrincipalsUpdateCmd)
}

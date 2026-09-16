package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var servicePrincipalsDeleteCmd = &cobra.Command{
	Use:   "delete <sp-id>",
	Short: "Delete a service principal and all its credentials",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		path := spPath(args[0])
		if dryRunActive() {
			return printDryRun(cmd, "DELETE", path, nil)
		}

		c, err := newServicePrincipalsClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		if _, err := c.Delete(cmd.Context(), path); err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted service principal: id=%s\n", args[0])
		return nil
	},
}

func init() {
	servicePrincipalsCmd.AddCommand(servicePrincipalsDeleteCmd)
}

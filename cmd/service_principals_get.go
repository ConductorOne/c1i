package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var servicePrincipalsGetCmd = &cobra.Command{
	Use:   "get <sp-id>",
	Short: "Get a single service principal by ID (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newServicePrincipalsClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		data, err := c.Get(cmd.Context(), spPath(args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeResource(cmd, data, "id")
	},
}

func init() {
	servicePrincipalsCmd.AddCommand(servicePrincipalsGetCmd)
}

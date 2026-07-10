package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var functionsGetCmd = &cobra.Command{
	Use:   "get <function-id>",
	Short: "Get a single function's metadata (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := client.New(cmd.Context(), baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		data, err := c.Get(cmd.Context(), "/api/v1/functions/"+args[0], nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeObject(cmd, data)
	},
}

func init() {
	functionsCmd.AddCommand(functionsGetCmd)
}

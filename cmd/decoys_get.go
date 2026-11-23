package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var decoysGetCmd = &cobra.Command{
	Use:   "get <decoy-id>",
	Short: "Get a single decoy by ID (pretty JSON)",
	Long: `Get a single decoy by ID (pretty JSON). Never prints secret material: that
is shown only once, by "decoys create" or "decoys rotate".`,
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
		data, err := c.Get(cmd.Context(), decoyPath(args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeResource(cmd, data, "id", "decoy")
	},
}

func init() {
	decoysCmd.AddCommand(decoysGetCmd)
}

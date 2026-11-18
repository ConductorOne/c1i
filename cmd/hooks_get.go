package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var hooksGetCmd = &cobra.Command{
	Use:   "get <hook-id>",
	Short: "Get a single hook by ID (pretty JSON)",
	Long: `Get a single hook by ID (pretty JSON). A deleted hook is still returned at
exit 0 and carries no marker; "hooks list" omits it.`,
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
		data, err := c.Get(cmd.Context(), hookPath(args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeResource(cmd, data, "id")
	},
}

func init() {
	hooksCmd.AddCommand(hooksGetCmd)
}

package cmd

import (
	"github.com/spf13/cobra"
)

var hooksGetCmd = &cobra.Command{
	Use:   "get <hook-id>",
	Short: "Get a single hook by ID (pretty JSON)",
	Long: `Get a single hook by ID (pretty JSON). A deleted hook is still returned at
exit 0 and carries no marker; "hooks list" omits it.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getResource(cmd, hookPath(args[0]), nil, "id", "hook")
	},
}

func init() {
	hooksCmd.AddCommand(hooksGetCmd)
}

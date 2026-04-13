package cmd

import "github.com/spf13/cobra"

var usersCmd = &cobra.Command{
	Use:   "users",
	Short: "Manage C1 users",
}

func init() {
	rootCmd.AddCommand(usersCmd)
}

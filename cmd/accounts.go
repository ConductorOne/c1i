package cmd

import "github.com/spf13/cobra"

var accountsCmd = &cobra.Command{
	Use:   "accounts",
	Short: "Manage application accounts",
}

func init() {
	rootCmd.AddCommand(accountsCmd)
}

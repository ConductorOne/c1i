package cmd

import "github.com/spf13/cobra"

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication credentials",
}

func init() {
	rootCmd.AddCommand(authCmd)
}

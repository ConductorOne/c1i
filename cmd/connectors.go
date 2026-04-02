package cmd

import "github.com/spf13/cobra"

var connectorsCmd = &cobra.Command{
	Use:   "connectors",
	Short: "Manage connectors",
}

func init() {
	rootCmd.AddCommand(connectorsCmd)
}

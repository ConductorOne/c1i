package cmd

import "github.com/spf13/cobra"

var appsCmd = &cobra.Command{
	Use:   "apps",
	Short: "Manage applications",
}

func init() {
	rootCmd.AddCommand(appsCmd)
}

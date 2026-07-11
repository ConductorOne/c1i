package cmd

import "github.com/spf13/cobra"

var requestsCmd = &cobra.Command{
	Use:   "requests",
	Short: "Create, list, and inspect access requests",
}

func init() {
	rootCmd.AddCommand(requestsCmd)
}

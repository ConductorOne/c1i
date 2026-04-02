package cmd

import "github.com/spf13/cobra"

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Search ConductorOne documentation and API reference",
}

func init() {
	rootCmd.AddCommand(docsCmd)
}
